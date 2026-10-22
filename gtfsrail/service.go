package gtfsrail

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"trains/gtfsrail/internal/gtfs"
	"trains/gtfsrail/internal/gtfsrt"
	"trains/gtfsrail/internal/stationlex"
)

// Interchanges are the stations where one-change journeys may change trains
// (SPEC.md §3.6). They are matched by exact name against the loaded feed at
// startup; a rename produces a warning rather than silently disabling routing.
var Interchanges = []string{
	"Bowen Hills station",
	"Roma Street station",
	"Central station",
	"Fortitude Valley station",
	"Eagle Junction station",
	"Northgate station",
	"Boggo Road station",
}

const (
	// MinConnection is the minimum time allowed to change trains.
	MinConnection = 4 * time.Minute
	// TightConnection is the buffer below which a change is flagged tight.
	TightConnection = 5 * time.Minute
)

// FeedURLs are the realtime feed endpoints.
type FeedURLs struct{ TripUpdates, VehiclePositions, Alerts string }

// Config configures Open.
type Config struct {
	// CacheDir holds the parsed static index, shared by every process.
	CacheDir string
	// StaticURL overrides the GTFS zip URL.
	StaticURL string
	// Feeds overrides the realtime feed URLs.
	Feeds FeedURLs
	// HTTPClient is used for every upstream request.
	HTTPClient *http.Client
	// Live polls realtime feeds in the background and refreshes the static
	// index hourly (serve mode). Otherwise Open fetches realtime once.
	Live bool
	// FetchTimeout bounds the one-shot realtime fetch; default 5 s.
	FetchTimeout time.Duration
	// ReservedWords are names a station must never normalise to, such as the
	// binary's subcommands.
	ReservedWords []string
	Logger        *slog.Logger
	// Now overrides the clock; for tests.
	Now func() time.Time
}

// realtime is the view of the realtime store the merge engine needs. Tests
// substitute a fixed snapshot.
type realtime interface {
	Snapshot() *gtfsrt.Snapshot
	Observations(tripID string) map[uint16]gtfsrt.Obs
	Health() gtfsrt.Health
	Degraded() bool
}

// state is everything derived from one static index. It is swapped as a whole
// when the static feed changes.
type state struct {
	ix           *gtfs.Index
	lex          *stationlex.Lexicon
	stations     []Station // sorted by name
	byID         map[StationID]int
	interchanges []StationID
	warnings     []string
}

// Service answers journey and trip queries.
type Service struct {
	cur    atomic.Pointer[state]
	rt     realtime
	cache  *gtfs.Cache
	now    func() time.Time
	log    *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Open loads the static index (from cache when fresh) and starts realtime.
// In one-shot mode a realtime failure is not an error: answers are marked
// Degraded and fall back to the schedule.
func Open(ctx context.Context, cfg Config) (*Service, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.FetchTimeout == 0 {
		cfg.FetchTimeout = 5 * time.Second
	}
	feeds := gtfsrt.DefaultFeeds
	if cfg.Feeds.TripUpdates != "" {
		feeds.TripUpdates = cfg.Feeds.TripUpdates
	}
	if cfg.Feeds.VehiclePositions != "" {
		feeds.VehiclePositions = cfg.Feeds.VehiclePositions
	}
	if cfg.Feeds.Alerts != "" {
		feeds.Alerts = cfg.Feeds.Alerts
	}

	cache := &gtfs.Cache{
		Dir: cfg.CacheDir, URL: cfg.StaticURL, Client: cfg.HTTPClient,
		Logger: cfg.Logger, Now: cfg.Now,
	}
	ix, err := cache.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load static timetable: %w", err)
	}
	store := gtfsrt.NewStore(feeds, cfg.HTTPClient, cfg.Logger, cfg.Now)
	s := &Service{rt: store, cache: cache, now: cfg.Now, log: cfg.Logger}
	st := newState(ix, cfg.ReservedWords)
	s.cur.Store(st)
	for _, w := range st.warnings {
		s.log.Warn("station validation", "warning", w)
	}

	if !cfg.Live {
		if err := store.FetchOnce(ctx, cfg.FetchTimeout); err != nil {
			s.log.Debug("realtime unavailable; answering from the schedule", "err", err)
		}
		return s, nil
	}
	bg, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Go(func() { store.Poll(bg) })
	s.wg.Go(func() { s.refreshStatic(bg, cfg.ReservedWords) })
	return s, nil
}

// newService builds a Service over a fixed index and realtime source; tests
// use it to run the merge engine without the network.
func newService(ix *gtfs.Index, rt realtime, now func() time.Time) *Service {
	s := &Service{rt: rt, now: now, log: slog.Default()}
	s.cur.Store(newState(ix, nil))
	return s
}

// Close stops background polling. It is safe to call on any Service.
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Service) refreshStatic(ctx context.Context, reserved []string) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cur := s.cur.Load()
		ix, changed, err := s.cache.Refresh(ctx, cur.ix)
		if err != nil {
			s.log.Warn("static timetable refresh failed", "err", err)
			continue
		}
		if changed {
			st := newState(ix, reserved)
			s.cur.Store(st)
			s.log.Info("static timetable updated", "etag", ix.ETag)
			for _, w := range st.warnings {
				s.log.Warn("station validation", "warning", w)
			}
		}
	}
}

func newState(ix *gtfs.Index, reserved []string) *state {
	entries := make([]stationlex.Entry, 0, len(ix.Stations))
	for _, st := range ix.Stations {
		entries = append(entries, stationlex.Entry{ID: st.ID, Name: st.Name})
	}
	lex := stationlex.New(entries, stationlex.DefaultAliases)

	stations := make([]Station, 0, len(ix.Stations))
	for _, st := range ix.Stations {
		stations = append(stations, Station{
			ID: StationID(st.ID), Name: st.Name, Lat: st.Lat, Lon: st.Lon,
			Aliases: lex.Aliases(st.ID),
		})
	}
	slices.SortFunc(stations, func(a, b Station) int { return cmp.Compare(a.Name, b.Name) })
	byID := make(map[StationID]int, len(stations))
	for i, st := range stations {
		byID[st.ID] = i
	}

	var interchanges []StationID
	for _, name := range Interchanges {
		for _, st := range stations {
			if st.Name == name {
				interchanges = append(interchanges, st.ID)
				break
			}
		}
	}
	return &state{
		ix: ix, lex: lex, stations: stations, byID: byID,
		interchanges: interchanges,
		warnings:     lex.Validate(Interchanges, reserved),
	}
}

// Warnings returns startup validation problems, such as an interchange or
// alias that no longer matches a station after a rename.
func (s *Service) Warnings() []string { return slices.Clone(s.cur.Load().warnings) }

// Stations returns every rail station, sorted by name.
func (s *Service) Stations() []Station { return slices.Clone(s.cur.Load().stations) }

// Station looks up a station by ID.
func (s *Service) Station(id StationID) (Station, bool) {
	st := s.cur.Load()
	i, ok := st.byID[id]
	if !ok {
		return Station{}, false
	}
	return st.stations[i], true
}

// AmbiguousError reports a query matching several stations.
type AmbiguousError struct {
	Query      string
	Candidates []Station
	msg        string
}

func (e *AmbiguousError) Error() string { return e.msg }

// UnknownError reports a query matching no station.
type UnknownError struct {
	Query       string
	Suggestions []Station
	msg         string
}

func (e *UnknownError) Error() string { return e.msg }

// ErrNotFound is returned for an unknown trip or shape.
var ErrNotFound = errors.New("not found")

// Resolve turns free text ("central", "roma st") into a station.
func (s *Service) Resolve(query string) (Station, error) {
	st := s.cur.Load()
	e, err := st.lex.Resolve(query)
	if err == nil {
		return st.stations[st.byID[StationID(e.ID)]], nil
	}
	toStations := func(es []stationlex.Entry) []Station {
		out := make([]Station, 0, len(es))
		for _, e := range es {
			if i, ok := st.byID[StationID(e.ID)]; ok {
				out = append(out, st.stations[i])
			}
		}
		return out
	}
	var amb *stationlex.AmbiguousError
	if errors.As(err, &amb) {
		return Station{}, &AmbiguousError{Query: query, Candidates: toStations(amb.Candidates), msg: amb.Error()}
	}
	var unk *stationlex.UnknownError
	if errors.As(err, &unk) {
		return Station{}, &UnknownError{Query: query, Suggestions: toStations(unk.Suggestions), msg: unk.Error()}
	}
	return Station{}, err
}

// Shape returns the simplified polyline for a trip shape.
func (s *Service) Shape(id ShapeID) ([]Point, bool) {
	pts, ok := s.cur.Load().ix.Shapes[string(id)]
	if !ok {
		return nil, false
	}
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[i] = Point{Lat: p.Lat, Lon: p.Lon}
	}
	return out, true
}

// FeedHealth describes one realtime feed.
type FeedHealth struct {
	LastSuccess time.Time
	LastAttempt time.Time
	LastError   string
	Fetches     int64
	NotModified int64
}

// Health describes all realtime feeds.
type Health struct {
	TripUpdates, VehiclePositions, Alerts FeedHealth
	Degraded                              bool
}

// Health reports realtime feed status.
func (s *Service) Health() Health {
	h := s.rt.Health()
	conv := func(f gtfsrt.FeedHealth) FeedHealth {
		return FeedHealth{
			LastSuccess: f.LastSuccess, LastAttempt: f.LastAttempt, LastError: f.LastError,
			Fetches: f.Fetches, NotModified: f.NotModified,
		}
	}
	return Health{
		TripUpdates: conv(h.TripUpdates), VehiclePositions: conv(h.VehiclePositions),
		Alerts: conv(h.Alerts), Degraded: s.rt.Degraded(),
	}
}

func (s *Service) freshness(snap *gtfsrt.Snapshot, now time.Time) Freshness {
	f := Freshness{GeneratedAt: now, Degraded: s.rt.Degraded() || snap.TripsHeader.IsZero()}
	if !snap.TripsHeader.IsZero() {
		f.DataAge = max(now.Sub(snap.TripsHeader), 0)
	}
	return f
}
