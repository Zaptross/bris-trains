package gtfsrt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Snapshot is an immutable view of the latest data from each feed. Callers must
// not mutate it.
type Snapshot struct {
	Trips         map[string]*TripUpdate // by trip_id
	Vehicles      map[string]*Vehicle    // by trip_id
	Alerts        []*Alert
	AlertsByRoute map[string][]*Alert
	AlertsByTrip  map[string][]*Alert
	AlertsByStop  map[string][]*Alert
	// FeedHeader.timestamp per feed; zero if never fetched.
	TripsHeader, VehiclesHeader, AlertsHeader time.Time
}

// NewSnapshot assembles a snapshot and builds the alert indexes. Nil maps are
// replaced by empty ones.
func NewSnapshot(trips map[string]*TripUpdate, tripsHeader time.Time, vehicles map[string]*Vehicle, vehiclesHeader time.Time, alerts []*Alert, alertsHeader time.Time) *Snapshot {
	if trips == nil {
		trips = map[string]*TripUpdate{}
	}
	if vehicles == nil {
		vehicles = map[string]*Vehicle{}
	}
	s := &Snapshot{
		Trips:          trips,
		Vehicles:       vehicles,
		Alerts:         alerts,
		AlertsByRoute:  map[string][]*Alert{},
		AlertsByTrip:   map[string][]*Alert{},
		AlertsByStop:   map[string][]*Alert{},
		TripsHeader:    tripsHeader,
		VehiclesHeader: vehiclesHeader,
		AlertsHeader:   alertsHeader,
	}
	for _, a := range alerts {
		for _, r := range a.Routes {
			s.AlertsByRoute[r] = append(s.AlertsByRoute[r], a)
		}
		for _, t := range a.Trips {
			s.AlertsByTrip[t] = append(s.AlertsByTrip[t], a)
		}
		for _, st := range a.Stops {
			s.AlertsByStop[st] = append(s.AlertsByStop[st], a)
		}
	}
	return s
}

// Obs is an observed stop event for one stop sequence of one trip.
type Obs struct {
	ArrTime, DepTime   int64 // unix seconds, 0 = unobserved
	ArrDelay, DepDelay int32
	Skipped            bool
}

// FeedHealth reports fetch status for one feed.
type FeedHealth struct {
	LastSuccess, LastAttempt time.Time
	LastError                string
	Fetches, NotModified     int64 // HTTP requests made; 304 responses
}

// Health reports fetch status for all three feeds.
type Health struct{ TripUpdates, VehiclePositions, Alerts FeedHealth }

// Polling cadence and thresholds (SPEC §3.4, §4).
const (
	tripPollInterval    = 15 * time.Second
	vehiclePollInterval = 15 * time.Second
	alertPollInterval   = 60 * time.Second
	pollFetchTimeout    = 10 * time.Second
	degradedAfter       = 90 * time.Second
	maxFeedBytes        = 64 << 20
	serviceDayStartHour = 3
)

type feedKind int

const (
	kindTrips feedKind = iota
	kindVehicles
	kindAlerts
)

func (k feedKind) String() string {
	return [...]string{"TripUpdates", "VehiclePositions", "Alerts"}[k]
}

var brisbane = func() *time.Location {
	if loc, err := time.LoadLocation("Australia/Brisbane"); err == nil {
		return loc
	}
	return time.FixedZone("AEST", 36000)
}()

// Store fetches the realtime feeds and publishes snapshots. It is safe for
// concurrent use.
type Store struct {
	urls   [3]string
	client *http.Client
	logger *slog.Logger
	now    func() time.Time

	snap atomic.Pointer[Snapshot]

	mu             sync.Mutex // guards the fields below
	etags          [3]string
	health         [3]FeedHealth
	trips          map[string]*TripUpdate
	tripsHeader    time.Time
	vehicles       map[string]*Vehicle
	vehiclesHeader time.Time
	alerts         []*Alert
	alertsHeader   time.Time

	obsMu     sync.Mutex
	obs       map[string]map[uint16]Obs
	obsWindow time.Time // start of the service window obs belongs to
}

// NewStore returns a store for feeds. A nil client, logger or now selects
// http.Client{Timeout: 30s}, slog.Default() and time.Now.
func NewStore(feeds Feeds, client *http.Client, logger *slog.Logger, now func() time.Time) *Store {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	s := &Store{
		urls:   [3]string{feeds.TripUpdates, feeds.VehiclePositions, feeds.Alerts},
		client: client,
		logger: logger,
		now:    now,
		obs:    map[string]map[uint16]Obs{},
	}
	s.snap.Store(NewSnapshot(nil, time.Time{}, nil, time.Time{}, nil, time.Time{}))
	return s
}

// FetchOnce fetches all three feeds concurrently, bounded by timeout. Feeds that
// succeed are published even if others fail; the returned error joins the
// failures.
func (s *Store) FetchOnce(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var errs [3]error
	var wg sync.WaitGroup
	for k := range feedKind(3) {
		wg.Go(func() { errs[k] = s.fetch(ctx, k) })
	}
	wg.Wait()
	return errors.Join(errs[:]...)
}

// Poll fetches every feed immediately, then TripUpdates and VehiclePositions
// every 15 s and Alerts every 60 s, until ctx is done. Fetch errors are logged
// and recorded in Health.
func (s *Store) Poll(ctx context.Context) {
	intervals := [3]time.Duration{tripPollInterval, vehiclePollInterval, alertPollInterval}
	var wg sync.WaitGroup
	for k := range feedKind(3) {
		wg.Go(func() {
			t := time.NewTicker(intervals[k])
			defer t.Stop()
			for {
				fctx, cancel := context.WithTimeout(ctx, pollFetchTimeout)
				err := s.fetch(fctx, k)
				cancel()
				if err != nil && ctx.Err() == nil {
					s.logger.Warn("realtime fetch failed", "feed", k.String(), "err", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		})
	}
	wg.Wait()
}

// Snapshot returns the latest published snapshot; never nil.
func (s *Store) Snapshot() *Snapshot { return s.snap.Load() }

// Health returns a copy of the per-feed fetch status.
func (s *Store) Health() Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Health{TripUpdates: s.health[kindTrips], VehiclePositions: s.health[kindVehicles], Alerts: s.health[kindAlerts]}
}

// Degraded reports whether TripUpdates or VehiclePositions has not succeeded
// within the last 90 s.
func (s *Store) Degraded() bool {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range [...]feedKind{kindTrips, kindVehicles} {
		ls := s.health[k].LastSuccess
		if ls.IsZero() || now.Sub(ls) > degradedAfter {
			return true
		}
	}
	return false
}

// Observations returns a copy of the observed stop events for tripID in the
// current service day, keyed by stop sequence.
func (s *Store) Observations(tripID string) map[uint16]Obs {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	s.rollObsLocked(s.now())
	out := make(map[uint16]Obs, len(s.obs[tripID]))
	for k, v := range s.obs[tripID] {
		out[k] = v
	}
	return out
}

// fetch performs one conditional GET of feed k, decodes and publishes it.
func (s *Store) fetch(ctx context.Context, k feedKind) error {
	err := s.fetchFeed(ctx, k)
	if err != nil {
		err = fmt.Errorf("fetch %s: %w", k, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.health[k].LastError = err.Error()
	}
	return err
}

func (s *Store) fetchFeed(ctx context.Context, k feedKind) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.urls[k], nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	s.mu.Lock()
	if e := s.etags[k]; e != "" {
		req.Header.Set("If-None-Match", e)
	}
	s.health[k].LastAttempt = s.now()
	s.health[k].Fetches++
	s.mu.Unlock()

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		s.mu.Lock()
		s.health[k].NotModified++
		s.health[k].LastSuccess = s.now()
		s.health[k].LastError = ""
		s.mu.Unlock()
		return nil
	case http.StatusOK:
	default:
		return fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxFeedBytes {
		return fmt.Errorf("body exceeds %d bytes", maxFeedBytes)
	}
	etag := resp.Header.Get("ETag")

	now := s.now()
	var (
		trips    map[string]*TripUpdate
		vehicles map[string]*Vehicle
		alerts   []*Alert
		header   time.Time
	)
	switch k {
	case kindTrips:
		trips, header, err = DecodeTripUpdates(body, now)
		if err == nil {
			s.recordObservations(trips, now)
		}
	case kindVehicles:
		vehicles, header, err = DecodeVehicles(body)
	case kindAlerts:
		alerts, header, err = DecodeAlerts(body)
	}
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch k {
	case kindTrips:
		s.trips, s.tripsHeader = trips, header
	case kindVehicles:
		s.vehicles, s.vehiclesHeader = vehicles, header
	case kindAlerts:
		s.alerts, s.alertsHeader = alerts, header
	}
	s.etags[k] = etag
	s.health[k].LastSuccess = now
	s.health[k].LastError = ""
	s.snap.Store(NewSnapshot(s.trips, s.tripsHeader, s.vehicles, s.vehiclesHeader, s.alerts, s.alertsHeader))
	return nil
}

// serviceWindowStart returns the most recent 03:00 Brisbane at or before t.
func serviceWindowStart(t time.Time) time.Time {
	l := t.In(brisbane)
	w := time.Date(l.Year(), l.Month(), l.Day(), serviceDayStartHour, 0, 0, 0, brisbane)
	if l.Before(w) {
		w = w.AddDate(0, 0, -1)
	}
	return w
}

// rollObsLocked clears the observation table when now has entered a new
// service window. obsMu must be held.
func (s *Store) rollObsLocked(now time.Time) {
	w := serviceWindowStart(now)
	if !w.Equal(s.obsWindow) {
		if !s.obsWindow.IsZero() {
			clear(s.obs)
		}
		s.obsWindow = w
	}
}

// recordObservations upserts every sequenced stop-time update. Fields absent
// from an update leave earlier observations intact.
func (s *Store) recordObservations(trips map[string]*TripUpdate, now time.Time) {
	s.obsMu.Lock()
	defer s.obsMu.Unlock()
	s.rollObsLocked(now)
	for id, tu := range trips {
		var m map[uint16]Obs
		for _, u := range tu.Updates {
			if !u.HasSeq {
				continue
			}
			if m == nil {
				if m = s.obs[id]; m == nil {
					m = map[uint16]Obs{}
					s.obs[id] = m
				}
			}
			key := uint16(u.Seq)
			o := m[key]
			if u.Arrival.HasTime {
				o.ArrTime = u.Arrival.Time
			}
			if u.Arrival.HasDelay {
				o.ArrDelay = u.Arrival.Delay
			}
			if u.Departure.HasTime {
				o.DepTime = u.Departure.Time
			}
			if u.Departure.HasDelay {
				o.DepDelay = u.Departure.Delay
			}
			o.Skipped = u.Skipped
			m[key] = o
		}
	}
}
