package gtfsrt

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// clock is a race-free injectable now.
type clock struct{ ns atomic.Int64 }

func newClock(t time.Time) *clock    { c := &clock{}; c.set(t); return c }
func (c *clock) set(t time.Time)     { c.ns.Store(t.UnixNano()) }
func (c *clock) add(d time.Duration) { c.ns.Add(int64(d)) }
func (c *clock) now() time.Time      { return time.Unix(0, c.ns.Load()) }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// feedServer serves the three synthetic feeds with ETag "v1"; status, when
// non-zero, overrides every response.
type feedServer struct {
	*httptest.Server
	status atomic.Int32
	bodies map[string][]byte
}

func newFeedServer(t *testing.T, header int64) *feedServer {
	t.Helper()
	alert := &gtfs.Alert{
		InformedEntity: []*gtfs.EntitySelector{{RouteId: proto.String("R1")}},
		HeaderText:     &gtfs.TranslatedString{Translation: []*gtfs.TranslatedString_Translation{{Text: proto.String("h")}}},
	}
	fs := &feedServer{bodies: map[string][]byte{
		"/tu":     marshal(t, []*gtfs.FeedEntity{tripEntity("T1", stu(1, header))}, header),
		"/vp":     marshal(t, []*gtfs.FeedEntity{vehicleEntity("T1", header)}, header),
		"/alerts": marshal(t, []*gtfs.FeedEntity{{Id: proto.String("A1"), Alert: alert}}, header),
	}}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st := fs.status.Load(); st != 0 {
			w.WriteHeader(int(st))
			return
		}
		b, ok := fs.bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *feedServer) feeds() Feeds {
	return Feeds{TripUpdates: fs.URL + "/tu", VehiclePositions: fs.URL + "/vp", Alerts: fs.URL + "/alerts"}
}

func TestStoreFetchOnce(t *testing.T) {
	const h = 1_800_000_000
	fs := newFeedServer(t, h)
	clk := newClock(time.Unix(h, 0))
	s := NewStore(fs.feeds(), nil, quietLogger(), clk.now)

	before := s.Snapshot()
	if before == nil || before.Trips == nil || before.Vehicles == nil || len(before.Trips) != 0 {
		t.Fatalf("pre-fetch snapshot: %+v", before)
	}
	if !s.Degraded() {
		t.Error("Degraded before any fetch should be true")
	}

	ctx := context.Background()
	if err := s.FetchOnce(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if snap.Trips["T1"] == nil || snap.Vehicles["T1"] == nil || len(snap.AlertsByRoute["R1"]) != 1 {
		t.Fatalf("snapshot not populated: %+v", snap)
	}
	if !snap.TripsHeader.Equal(time.Unix(h, 0)) {
		t.Errorf("TripsHeader = %v", snap.TripsHeader)
	}
	if obs := s.Observations("T1"); obs[1].ArrTime != h || obs[1].ArrDelay != 30 {
		t.Errorf("observations: %+v", obs)
	}
	if s.Degraded() {
		t.Error("Degraded right after success")
	}

	// 304 path: data kept, NotModified counted, success refreshed.
	clk.add(30 * time.Second)
	if err := s.FetchOnce(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	hl := s.Health()
	for name, fh := range map[string]FeedHealth{"tu": hl.TripUpdates, "vp": hl.VehiclePositions, "alerts": hl.Alerts} {
		if fh.Fetches != 2 || fh.NotModified != 1 || !fh.LastSuccess.Equal(clk.now()) || fh.LastError != "" {
			t.Errorf("%s health after 304: %+v", name, fh)
		}
	}
	if s.Snapshot().Trips["T1"] == nil {
		t.Error("304 lost data")
	}

	// 500 path: data kept, LastError set, LastSuccess unchanged.
	lastOK := clk.now()
	clk.add(30 * time.Second)
	fs.status.Store(http.StatusInternalServerError)
	if err := s.FetchOnce(ctx, 5*time.Second); err == nil {
		t.Fatal("want error on 500")
	}
	hl = s.Health()
	if hl.TripUpdates.LastError == "" || !hl.TripUpdates.LastSuccess.Equal(lastOK) || !hl.TripUpdates.LastAttempt.Equal(clk.now()) {
		t.Errorf("health after 500: %+v", hl.TripUpdates)
	}
	if s.Snapshot().Trips["T1"] == nil || s.Snapshot().Vehicles["T1"] == nil {
		t.Error("500 lost data")
	}

	// Degraded boundary: 90 s since last success is fine, 91 s is not.
	clk.set(lastOK.Add(90 * time.Second))
	if s.Degraded() {
		t.Error("Degraded at 90 s")
	}
	clk.set(lastOK.Add(91 * time.Second))
	if !s.Degraded() {
		t.Error("not Degraded at 91 s")
	}
}

func TestStorePartialFailure(t *testing.T) {
	const h = 1_800_000_000
	fs := newFeedServer(t, h)
	feeds := fs.feeds()
	feeds.Alerts = fs.URL + "/missing"
	s := NewStore(feeds, nil, quietLogger(), newClock(time.Unix(h, 0)).now)
	if err := s.FetchOnce(context.Background(), 5*time.Second); err == nil {
		t.Fatal("want error for missing alerts feed")
	}
	snap := s.Snapshot()
	if snap.Trips["T1"] == nil || snap.Vehicles["T1"] == nil || len(snap.Alerts) != 0 || !snap.AlertsHeader.IsZero() {
		t.Errorf("partial publish: %+v", snap)
	}
	if s.Health().Alerts.LastError == "" {
		t.Error("alerts LastError not set")
	}
}

func TestPollConcurrentReadersAndCancel(t *testing.T) {
	const h = 1_800_000_000
	fs := newFeedServer(t, h)
	s := NewStore(fs.feeds(), nil, quietLogger(), newClock(time.Unix(h, 0)).now)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Poll(ctx); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for s.Snapshot().Trips["T1"] == nil || s.Snapshot().Vehicles["T1"] == nil || len(s.Snapshot().Alerts) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Poll did not publish")
		}
		time.Sleep(5 * time.Millisecond)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 2000 {
				snap := s.Snapshot()
				_ = len(snap.Trips) + len(snap.Vehicles) + len(snap.AlertsByRoute["R1"])
				_ = s.Observations("T1")
				_ = s.Health()
				_ = s.Degraded()
			}
		})
	}
	wg.Wait()

	hl := s.Health()
	for name, fh := range map[string]FeedHealth{"tu": hl.TripUpdates, "vp": hl.VehiclePositions, "alerts": hl.Alerts} {
		if fh.Fetches != 1 {
			t.Errorf("%s: readers caused fetches: Fetches = %d, want 1", name, fh.Fetches)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Poll did not return after cancel")
	}
}

func TestPollCancelDuringFetch(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	s := NewStore(Feeds{TripUpdates: srv.URL, VehiclePositions: srv.URL, Alerts: srv.URL}, nil, quietLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Poll(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Poll did not return promptly while a fetch was in flight")
	}
}

func TestObservationReset(t *testing.T) {
	day := func(d, hh, mm, ss int) time.Time { return time.Date(2026, 10, d, hh, mm, ss, 0, brisbane) }
	clk := newClock(day(23, 22, 0, 0))
	s := NewStore(Feeds{}, nil, quietLogger(), clk.now)

	obsAt := day(23, 21, 58, 0).Unix()
	trips := map[string]*TripUpdate{"T1": {TripID: "T1", Updates: []StopTimeUpdate{
		{Seq: 4, HasSeq: true, HasArrival: true, Arrival: Event{Time: obsAt, HasTime: true, Delay: 60, HasDelay: true}},
		{Seq: 5, HasSeq: true, Skipped: true},
		{StopID: "noseq", HasArrival: true, Arrival: Event{Time: obsAt, HasTime: true}},
	}}}
	s.recordObservations(trips, clk.now())

	// A later update that omits the arrival time must not erase it.
	s.recordObservations(map[string]*TripUpdate{"T1": {TripID: "T1", Updates: []StopTimeUpdate{
		{Seq: 4, HasSeq: true, HasDeparture: true, Departure: Event{Time: obsAt + 60, HasTime: true}},
	}}}, clk.now())

	want := map[uint16]Obs{4: {ArrTime: obsAt, ArrDelay: 60, DepTime: obsAt + 60}, 5: {Skipped: true}}
	check := func(label string, want map[uint16]Obs) {
		t.Helper()
		got := s.Observations("T1")
		if len(got) != len(want) {
			t.Fatalf("%s: got %+v, want %+v", label, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: seq %d = %+v, want %+v", label, k, got[k], v)
			}
		}
	}
	check("22:00", want)

	clk.set(day(24, 2, 59, 59))
	check("02:59:59", want)

	clk.set(day(24, 3, 0, 0))
	check("03:00:00", nil)

	// Mutating the returned copy must not affect the store.
	clk.set(day(24, 4, 0, 0))
	s.recordObservations(trips, clk.now())
	got := s.Observations("T1")
	delete(got, 4)
	if _, ok := s.Observations("T1")[4]; !ok {
		t.Error("Observations did not return a copy")
	}
}

func TestServiceWindowStart(t *testing.T) {
	cases := []struct{ at, want time.Time }{
		{time.Date(2026, 10, 24, 2, 59, 59, 0, brisbane), time.Date(2026, 10, 23, 3, 0, 0, 0, brisbane)},
		{time.Date(2026, 10, 24, 3, 0, 0, 0, brisbane), time.Date(2026, 10, 24, 3, 0, 0, 0, brisbane)},
		{time.Date(2026, 10, 23, 17, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 3, 0, 0, 0, brisbane)}, // 03:00 Brisbane
	}
	for _, c := range cases {
		if got := serviceWindowStart(c.at); !got.Equal(c.want) {
			t.Errorf("serviceWindowStart(%v) = %v, want %v", c.at, got, c.want)
		}
	}
}
