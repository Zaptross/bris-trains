package gtfsrail

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	rtpb "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"

	"trains/gtfsrail/internal/gtfs"
	"trains/gtfsrail/internal/gtfsrt"
)

// Golden cases G1–G11 from PLAN.md P4. Every case runs against the real
// fixture (testdata/static.zip) with realtime built as GTFS-RT protobuf and
// decoded by the production decoder, so decode and merge are exercised
// together.

const (
	te95     = "39764967-QR 26_27-44634-TE95" // Beenleigh → Ferny Grove, 39 stops
	run1031  = "39764262-QR 26_27-44634-1031" // Cleveland 23:39 → Bowen Hills 24:48
	thursday = "20261022"
)

var thu = Date{Year: 2026, Month: time.October, Day: 22}

var (
	fixtureOnce sync.Once
	fixtureIx   *gtfs.Index
	fixtureErr  error
)

func fixture(t *testing.T) *gtfs.Index {
	t.Helper()
	fixtureOnce.Do(func() { fixtureIx, fixtureErr = gtfs.ParseFile("../testdata/static.zip") })
	if fixtureErr != nil {
		t.Fatalf("parse fixture: %v", fixtureErr)
	}
	return fixtureIx
}

// fakeRT is a fixed realtime snapshot.
type fakeRT struct {
	snap     *gtfsrt.Snapshot
	obs      map[string]map[uint16]gtfsrt.Obs
	degraded bool
}

func (f *fakeRT) Snapshot() *gtfsrt.Snapshot { return f.snap }
func (f *fakeRT) Observations(trip string) map[uint16]gtfsrt.Obs {
	out := map[uint16]gtfsrt.Obs{}
	for k, v := range f.obs[trip] {
		out[k] = v
	}
	return out
}
func (f *fakeRT) Health() gtfsrt.Health { return gtfsrt.Health{} }
func (f *fakeRT) Degraded() bool        { return f.degraded }

func brisbane(date Date, hh, mm, ss int) time.Time {
	return time.Date(int(date.Year), date.Month, int(date.Day), hh, mm, ss, 0, Brisbane)
}

// sched returns a fixture stop time as absolute times on date.
func sched(t *testing.T, ix *gtfs.Index, tripID string, seq uint16, date Date) (stopID string, arr, dep time.Time) {
	t.Helper()
	trip := ix.Trips[tripID]
	if trip == nil {
		t.Fatalf("trip %s not in fixture", tripID)
	}
	for _, st := range ix.TripStopTimes(trip) {
		if st.Seq == seq {
			m := date.Midnight(Brisbane)
			return st.StopID, m.Add(secs(st.Arrival)), m.Add(secs(st.Departure))
		}
	}
	t.Fatalf("trip %s has no seq %d", tripID, seq)
	return "", time.Time{}, time.Time{}
}

// tuBuilder assembles one TripUpdate entity.
type tuBuilder struct {
	t    *testing.T
	ix   *gtfs.Index
	tu   *rtpb.TripUpdate
	date Date
}

func newTU(t *testing.T, ix *gtfs.Index, tripID, routeID string, rel rtpb.TripDescriptor_ScheduleRelationship, date Date) *tuBuilder {
	return &tuBuilder{t: t, ix: ix, date: date, tu: &rtpb.TripUpdate{Trip: &rtpb.TripDescriptor{
		TripId: proto.String(tripID), RouteId: proto.String(routeID),
		StartDate: proto.String(date.GTFS()), ScheduleRelationship: rel.Enum(),
	}}}
}

// delayed adds stop time updates for seqs with the delay applied to the
// fixture schedule, carrying both time and delay as the live feed does.
func (b *tuBuilder) delayed(delay int32, seqs ...uint16) *tuBuilder {
	for _, seq := range seqs {
		stop, arr, dep := sched(b.t, b.ix, b.tu.Trip.GetTripId(), seq, b.date)
		d := time.Duration(delay) * time.Second
		b.tu.StopTimeUpdate = append(b.tu.StopTimeUpdate, &rtpb.TripUpdate_StopTimeUpdate{
			StopSequence: proto.Uint32(uint32(seq)), StopId: proto.String(stop),
			Arrival:   &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay), Time: proto.Int64(arr.Add(d).Unix())},
			Departure: &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay), Time: proto.Int64(dep.Add(d).Unix())},
		})
	}
	return b
}

// stamped overrides start_date/start_time, as the live feed sets them.
func (b *tuBuilder) stamped(startDate, startTime string) *tuBuilder {
	b.tu.Trip.StartDate = proto.String(startDate)
	b.tu.Trip.StartTime = proto.String(startTime)
	return b
}

func (b *tuBuilder) skipped(seqs ...uint16) *tuBuilder {
	for _, seq := range seqs {
		stop, _, _ := sched(b.t, b.ix, b.tu.Trip.GetTripId(), seq, b.date)
		b.tu.StopTimeUpdate = append(b.tu.StopTimeUpdate, &rtpb.TripUpdate_StopTimeUpdate{
			StopSequence: proto.Uint32(uint32(seq)), StopId: proto.String(stop),
			ScheduleRelationship: rtpb.TripUpdate_StopTimeUpdate_SKIPPED.Enum(),
		})
	}
	return b
}

// raw adds an update for a stop that need not exist in the fixture.
func (b *tuBuilder) raw(seq uint32, stop string, at time.Time) *tuBuilder {
	b.tu.StopTimeUpdate = append(b.tu.StopTimeUpdate, &rtpb.TripUpdate_StopTimeUpdate{
		StopSequence: proto.Uint32(seq), StopId: proto.String(stop),
		Arrival:   &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(0), Time: proto.Int64(at.Unix())},
		Departure: &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(0), Time: proto.Int64(at.Unix())},
	})
	return b
}

type vehicleSpec struct {
	trip string
	seq  uint32
	age  time.Duration // behind the feed header
}

// realtimeAt decodes TripUpdates and VehiclePositions through the production
// decoders, with feed headers at now.
func realtimeAt(t *testing.T, now time.Time, tus []*tuBuilder, vehicles ...vehicleSpec) *fakeRT {
	t.Helper()
	header := &rtpb.FeedHeader{
		GtfsRealtimeVersion: proto.String("2.0"),
		Incrementality:      rtpb.FeedHeader_FULL_DATASET.Enum(),
		Timestamp:           proto.Uint64(uint64(now.Unix())),
	}
	tuFeed := &rtpb.FeedMessage{Header: header}
	for i, b := range tus {
		tuFeed.Entity = append(tuFeed.Entity, &rtpb.FeedEntity{Id: proto.String("TU" + string(rune('A'+i))), TripUpdate: b.tu})
	}
	vpFeed := &rtpb.FeedMessage{Header: header}
	for i, v := range vehicles {
		vpFeed.Entity = append(vpFeed.Entity, &rtpb.FeedEntity{Id: proto.String("VP" + string(rune('A'+i))), Vehicle: &rtpb.VehiclePosition{
			Trip:                &rtpb.TripDescriptor{TripId: proto.String(v.trip)},
			Vehicle:             &rtpb.VehicleDescriptor{Id: proto.String("veh"), Label: proto.String("RUN")},
			Position:            &rtpb.Position{Latitude: proto.Float32(-27.4), Longitude: proto.Float32(153.0)},
			CurrentStopSequence: proto.Uint32(v.seq),
			CurrentStatus:       rtpb.VehiclePosition_IN_TRANSIT_TO.Enum(),
			Timestamp:           proto.Uint64(uint64(now.Add(-v.age).Unix())),
		}})
	}
	tuBytes, err := proto.Marshal(tuFeed)
	if err != nil {
		t.Fatal(err)
	}
	vpBytes, err := proto.Marshal(vpFeed)
	if err != nil {
		t.Fatal(err)
	}
	trips, tuHeader, err := gtfsrt.DecodeTripUpdates(tuBytes, now)
	if err != nil {
		t.Fatal(err)
	}
	vehs, vpHeader, err := gtfsrt.DecodeVehicles(vpBytes)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeRT{snap: gtfsrt.NewSnapshot(trips, tuHeader, vehs, vpHeader, nil, time.Time{}), obs: map[string]map[uint16]gtfsrt.Obs{}}
}

func noRealtime() *fakeRT {
	return &fakeRT{snap: gtfsrt.NewSnapshot(nil, time.Time{}, nil, time.Time{}, nil, time.Time{}), degraded: true}
}

func leg(t *testing.T, s *Service, trip string, date Date) *Leg {
	t.Helper()
	l, _, err := s.Leg(context.Background(), TripID(trip), date, LegOptions{})
	if err != nil {
		t.Fatalf("Leg(%s): %v", trip, err)
	}
	return l
}

func callAt(t *testing.T, l *Leg, seq uint16) *Call {
	t.Helper()
	for i := range l.Calls {
		if l.Calls[i].Sequence == seq {
			return &l.Calls[i]
		}
	}
	t.Fatalf("no call with seq %d", seq)
	return nil
}

func TestG1TrimmedScheduledUpdate(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 4, 0)
	// A SCHEDULED update that starts mid-trip (seq 29 of 39) and stops early.
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_SCHEDULED, thu).delayed(34, 29, 30, 31)
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	l := leg(t, s, te95, thu)

	for seq := uint16(1); seq <= 28; seq++ {
		c := callAt(t, l, seq)
		if c.Source != Scheduled || !c.Expected.Equal(c.Scheduled) {
			t.Errorf("seq %d before first update: source %v expected %v scheduled %v; want Scheduled at schedule", seq, c.Source, c.Expected, c.Scheduled)
		}
	}
	for seq := uint16(29); seq <= 31; seq++ {
		if c := callAt(t, l, seq); c.Source != Predicted || c.Delay() != 34*time.Second {
			t.Errorf("seq %d covered: source %v delay %v; want Predicted +34s", seq, c.Source, c.Delay())
		}
	}
	for seq := uint16(32); seq <= 39; seq++ {
		if c := callAt(t, l, seq); c.Source != Projected || c.Delay() != 34*time.Second {
			t.Errorf("seq %d after last update: source %v delay %v; want Projected carrying +34s", seq, c.Source, c.Delay())
		}
	}
	if c := callAt(t, l, 29); c.Position != Current {
		t.Errorf("Windsor (seq 29, due 20:05:34) position %v at 20:04; want Current", c.Position)
	}
	if c := callAt(t, l, 28); c.Position != Passed {
		t.Errorf("Bowen Hills (seq 28, 20:01) position %v at 20:04; want Passed", c.Position)
	}
}

func TestG2ObservationTableKeepsTrimmedPast(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 4, 0)
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_SCHEDULED, thu).delayed(34, 29, 30, 31)
	rt := realtimeAt(t, now, []*tuBuilder{tu})
	// Seeded as an earlier full update would have left it: every passed stop
	// observed 20 s late.
	rt.obs[te95] = map[uint16]gtfsrt.Obs{}
	for seq := uint16(1); seq <= 28; seq++ {
		_, arr, dep := sched(t, ix, te95, seq, thu)
		rt.obs[te95][seq] = gtfsrt.Obs{ArrTime: arr.Unix() + 20, DepTime: dep.Unix() + 20, ArrDelay: 20, DepDelay: 20}
	}
	s := newService(ix, rt, func() time.Time { return now })
	l := leg(t, s, te95, thu)

	for seq := uint16(1); seq <= 28; seq++ {
		c := callAt(t, l, seq)
		if c.Source != Observed || c.Actual == nil || c.Delay() != 20*time.Second {
			t.Errorf("seq %d: source %v actual %v delay %v; want Observed +20s from the observation table", seq, c.Source, c.Actual, c.Delay())
		}
	}
}

func TestG3ReplacementFullStopList(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 4, 0)
	seqs := make([]uint16, 39)
	for i := range seqs {
		seqs[i] = uint16(i + 1)
	}
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_REPLACEMENT, thu).delayed(60, seqs...)
	// The live feed carries the platform assignment; move Windsor to another
	// platform of the same station.
	windsor := ix.Stops[tu.tu.StopTimeUpdate[28].GetStopId()]
	var other *gtfs.Stop
	for _, p := range ix.Stations[windsor.Parent].Platforms {
		if p != windsor.ID {
			other = ix.Stops[p]
			break
		}
	}
	if other == nil {
		t.Fatal("Windsor has a single platform in the fixture; pick another station")
	}
	tu.tu.StopTimeUpdate[28].StopTimeProperties = &rtpb.TripUpdate_StopTimeUpdate_StopTimeProperties{AssignedStopId: proto.String(other.ID)}

	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	l, _, err := s.Leg(context.Background(), te95, thu, LegOptions{From: StationID(windsor.Parent)})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range l.Calls {
		want := Predicted
		if c.Position == Passed {
			want = Observed
		}
		if c.Source != want || c.Delay() != time.Minute {
			t.Errorf("seq %d (%v): source %v delay %v; want %v +60s", c.Sequence, c.Position, c.Source, c.Delay(), want)
		}
	}
	if got := callAt(t, l, 29).Platform; got != other.Platform {
		t.Errorf("Windsor platform %q; want assigned platform %q", got, other.Platform)
	}
	// 60 s is the first second of "late" (SPEC.md §3.5).
	if l.Punctuality != Late || l.DelaySeconds != 60 {
		t.Errorf("boarding at Windsor: punctuality %v delay %d; want late, 60", l.Punctuality, l.DelaySeconds)
	}
}

func TestG4AddedTrip(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 19, 50, 0)
	tu := newTU(t, ix, "ADDED-1", "BNFG-5251", rtpb.TripDescriptor_ADDED, thu)
	// Five real stops of TE95, run 10 min after it.
	for seq := uint16(27); seq <= 31; seq++ {
		stop, _, dep := sched(t, ix, te95, seq, thu)
		tu.raw(uint32(seq-26), stop, dep.Add(10*time.Minute))
	}
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })

	l := leg(t, s, "ADDED-1", thu)
	if !l.Added || l.ShapeID != "" || len(l.Calls) != 5 {
		t.Fatalf("added leg: added %v shape %q calls %d; want added, no shape, 5 calls", l.Added, l.ShapeID, len(l.Calls))
	}
	if l.Calls[0].Name != "Fortitude Valley station" || l.Headsign != "Newmarket station" {
		t.Errorf("first call %q headsign %q", l.Calls[0].Name, l.Headsign)
	}

	valley, windsor := StationID(l.Calls[0].Station), StationID(l.Calls[2].Station)
	js, _, err := s.Journeys(context.Background(), valley, windsor, now, JourneyOptions{Limit: 50, DirectOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range js {
		found = found || j.Legs[0].TripID == "ADDED-1"
	}
	if !found {
		t.Error("ADDED trip missing from Fortitude Valley → Windsor journeys")
	}
}

func TestG5SkippedStopKeepsCarriedDelay(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 4, 0)
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_SCHEDULED, thu).delayed(120, 29).skipped(30)
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	l := leg(t, s, te95, thu)

	if c := callAt(t, l, 30); !c.Skipped {
		t.Errorf("Wilston (seq 30) skipped = %v; want true", c.Skipped)
	}
	if c := callAt(t, l, 31); c.Source != Projected || c.Delay() != 2*time.Minute {
		t.Errorf("Newmarket (seq 31): source %v delay %v; want Projected +2m (skip must not reset the delay)", c.Source, c.Delay())
	}
	// Boarding at the skipped stop is reported as skipping.
	l, _, err := s.Leg(context.Background(), te95, thu, LegOptions{From: callAt(t, l, 30).Station})
	if err != nil {
		t.Fatal(err)
	}
	if l.Punctuality != Skipping {
		t.Errorf("boarding at a skipped stop: %v; want skipping", l.Punctuality)
	}
}

func TestG6UnknownStop(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 19, 50, 0)
	_, _, dep := sched(t, ix, te95, 28, thu)
	tu := newTU(t, ix, "ADDED-2", "VLBD-5251", rtpb.TripDescriptor_ADDED, thu)
	tu.raw(1, "600000", dep.Add(5*time.Minute)) // Bowen Hills
	tu.raw(2, "600192", dep.Add(9*time.Minute)) // absent from every static feed (SPEC.md §2.3)
	tu.raw(3, "600013", dep.Add(12*time.Minute))
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })

	l := leg(t, s, "ADDED-2", thu)
	if len(l.Calls) != 3 {
		t.Fatalf("calls %d; want 3 (the unknown stop must not be dropped)", len(l.Calls))
	}
	if c := l.Calls[1]; c.Name != "Unknown stop 600192" || c.Station != "" {
		t.Errorf("unknown stop rendered as %q (station %q)", c.Name, c.Station)
	}
}

func TestG7StaleVehicleFallsBackToTime(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 4, 0)
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_SCHEDULED, thu).delayed(30, 29, 30, 31)

	// 6.1 h old, as observed live: ignored for positioning.
	stale := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}, vehicleSpec{te95, 35, 22102 * time.Second}), func() time.Time { return now })
	l := leg(t, stale, te95, thu)
	if l.Vehicle == nil || !l.Vehicle.Stale {
		t.Fatalf("vehicle %+v; want present and stale", l.Vehicle)
	}
	if c := callAt(t, l, 29); c.Position != Current {
		t.Errorf("stale fix at seq 35: Windsor (seq 29) position %v; want Current by time", c.Position)
	}

	// A fresh fix is authoritative over the clock.
	fresh := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}, vehicleSpec{te95, 31, 10 * time.Second}), func() time.Time { return now })
	l = leg(t, fresh, te95, thu)
	if c := callAt(t, l, 31); c.Position != Current {
		t.Errorf("fresh fix at seq 31: position %v; want Current", c.Position)
	}
	if c := callAt(t, l, 30); c.Position != Passed || c.Source != Observed {
		t.Errorf("seq 30 behind a fresh fix: %v/%v; want Passed/Observed", c.Position, c.Source)
	}
}

func TestG8PostMidnightServiceDay(t *testing.T) {
	ix := fixture(t)
	// Friday 00:30: run 1031 belongs to Thursday's service and runs to 24:48.
	fri := thu.AddDays(1)
	now := brisbane(fri, 0, 30, 0)
	s := newService(ix, noRealtime(), func() time.Time { return now })

	l := leg(t, s, run1031, thu)
	last := l.Calls[len(l.Calls)-1]
	if want := brisbane(fri, 0, 48, 0); !last.Scheduled.Equal(want) {
		t.Fatalf("final call %s; want %s (24:48 on Thursday's service is 00:48 Friday)", last.Scheduled, want)
	}
	var origin StationID
	for _, c := range l.Calls {
		if c.Position != Passed && c.Scheduled.Before(last.Scheduled) {
			origin = c.Station
			break
		}
	}
	if origin == "" {
		t.Fatal("run 1031 has no upcoming stop before its terminus at 00:30")
	}
	js, _, err := s.Journeys(context.Background(), origin, last.Station, now, JourneyOptions{Limit: 20, DirectOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range js {
		if j.Legs[0].TripID == run1031 {
			if j.Legs[0].ServiceDate != thu {
				t.Errorf("run 1031 service date %s; want %s", j.Legs[0].ServiceDate, thu)
			}
			return
		}
	}
	t.Errorf("run 1031 missing at 00:30 Friday: Thursday's service must be searched for post-midnight trips")
}

func TestG9NoRealtimeIsNeverOnTime(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 20, 0, 0)
	s := newService(ix, noRealtime(), func() time.Time { return now })
	js, fresh, err := s.Journeys(context.Background(), "place_censta", "place_romsta", now, JourneyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(js) != 5 || !fresh.Degraded {
		t.Fatalf("got %d journeys, degraded %v; want 5 and degraded", len(js), fresh.Degraded)
	}
	for _, j := range js {
		if j.Punctuality != NoRealtime {
			t.Errorf("%s at %s: punctuality %v; want scheduled (no realtime)", j.Legs[0].TripID, j.Depart().Format("15:04"), j.Punctuality)
		}
		for _, c := range j.Legs[0].Calls {
			if c.Position != Passed && c.Source != Scheduled {
				t.Errorf("call %s source %v without realtime", c.Name, c.Source)
			}
		}
	}
}

func TestG10ErodedConnectionIsReplaced(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 19, 0, 0)
	const fg, cl = StationID("place_fersta"), StationID("place_clesta")
	base := newService(ix, noRealtime(), func() time.Time { return now })
	if _, ok := base.Station(fg); !ok {
		t.Fatalf("station %s missing; check Ferny Grove's ID in testdata/stations.csv", fg)
	}
	js, _, err := base.Journeys(context.Background(), fg, cl, now, JourneyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(js) == 0 || js[0].Change == nil {
		t.Fatalf("Ferny Grove → Cleveland: %d journeys; want one-change journeys (no direct route exists)", len(js))
	}
	first := js[0]
	l1, l2 := first.Legs[0], first.Legs[1]

	// Delay the first train so its connection falls to 3 minutes.
	delay := int32((first.Change.Buffer - 3*time.Minute) / time.Second)
	board := l1.Calls[l1.Board].Sequence
	tu := newTU(t, ix, string(l1.TripID), ix.Trips[string(l1.TripID)].RouteID, rtpb.TripDescriptor_SCHEDULED, l1.ServiceDate).delayed(delay, board)
	late := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	js, _, err = late.Journeys(context.Background(), fg, cl, now, JourneyOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range js {
		if j.Change != nil && j.Change.Buffer < MinConnection {
			t.Errorf("journey with a %v connection offered; minimum is %v", j.Change.Buffer, MinConnection)
		}
		if j.Legs[0].TripID == l1.TripID && len(j.Legs) == 2 && j.Legs[1].TripID == l2.TripID {
			t.Errorf("eroded pairing %s → %s still offered", l1.TripID, l2.TripID)
		}
	}
}

func TestG11DirectWinsTies(t *testing.T) {
	base := brisbane(thu, 20, 0, 0)
	mk := func(dep, arr int) Leg {
		d, a := base.Add(time.Duration(dep)*time.Minute), base.Add(time.Duration(arr)*time.Minute)
		return Leg{Calls: []Call{{Expected: d}, {Expected: a, expectedArrival: a}}, Board: 0, Alight: 1, Punctuality: OnTime}
	}
	direct := newJourney([]Leg{mk(10, 40)}, nil)
	tie := newJourney([]Leg{mk(5, 20), mk(25, 40)}, &Change{Buffer: 5 * time.Minute})    // leaves earlier, same arrival
	later := newJourney([]Leg{mk(12, 20), mk(25, 40)}, &Change{Buffer: 5 * time.Minute}) // leaves later, same arrival
	got := prune([]Journey{direct, tie, later})
	if len(got) != 2 || got[0].Change != nil || got[1].Depart() != later.Depart() {
		t.Fatalf("prune kept %d journeys; want the direct one and the later-leaving change only", len(got))
	}

	// On real data the radial network means a change never beats Central →
	// Roma Street, so only direct trains appear.
	ix := fixture(t)
	now := brisbane(thu, 20, 0, 0)
	s := newService(ix, noRealtime(), func() time.Time { return now })
	js, _, err := s.Journeys(context.Background(), "place_censta", "place_romsta", now, JourneyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, j := range js {
		if j.Change != nil {
			t.Errorf("Central → Roma Street offered a change at %s", j.Change.Name)
		}
	}
}

const d510 = "39765169-QR 26_27-44635-D510" // Nambour 05:08 → Ipswich 07:56, 44 stops

var fri = Date{Year: 2026, Month: time.October, Day: 23}

// event adds one stop time update at an explicit stop, time and delay.
func (b *tuBuilder) event(seq uint32, stop string, at time.Time, delay int32) *tuBuilder {
	b.tu.StopTimeUpdate = append(b.tu.StopTimeUpdate, &rtpb.TripUpdate_StopTimeUpdate{
		StopSequence: proto.Uint32(seq), StopId: proto.String(stop),
		Arrival:   &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay), Time: proto.Int64(at.Unix())},
		Departure: &rtpb.TripUpdate_StopTimeEvent{Delay: proto.Int32(delay), Time: proto.Int64(at.Unix())},
	})
	return b
}

// TestRenumberedReplacement reproduces the live D510 snapshot at 06:42:30 on
// 2026-10-23. Translink re-sent the trip as a REPLACEMENT numbered from its
// current position: sequence 1 was Central (static seq 21), 2 Roma Street,
// and so on. Matched by sequence, Central's update landed on Nambour: the
// first call was renamed "Central station", kept Nambour's 05:08 schedule,
// and the train read 94 min late. Sequences must only match at the same
// station, with renumbered updates applied by station instead.
func TestRenumberedReplacement(t *testing.T) {
	ix := fixture(t)
	now := brisbane(fri, 6, 43, 12)
	tu := newTU(t, ix, d510, "NAIP-5252", rtpb.TripDescriptor_REPLACEMENT, fri)
	for i, static := range []uint16{21, 22, 23} { // Central, Roma Street, Milton
		stop, _, dep := sched(t, ix, d510, static, fri)
		tu.event(uint32(i+1), stop, dep.Add(2*time.Minute), 120)
	}
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	l := leg(t, s, d510, fri)

	ref := ix.Trips[d510]
	for i, st := range ix.TripStopTimes(ref) {
		station, _ := ix.StationOf(st.StopID)
		if c := l.Calls[i]; string(c.Station) != station.ID {
			t.Errorf("seq %d is %s in the timetable but rendered as %s", st.Seq, station.Name, c.Name)
		}
	}
	if c := callAt(t, l, 1); c.Source == Predicted || c.Source == Observed {
		t.Errorf("Nambour (seq 1) took Central's update: source %v delay %v", c.Source, c.Delay())
	}
	for _, seq := range []uint16{21, 22, 23} {
		if c := callAt(t, l, seq); c.Source != Predicted || c.Delay() != 2*time.Minute {
			t.Errorf("%s (seq %d): source %v delay %v; want its renumbered update, +2m", c.Name, seq, c.Source, c.Delay())
		}
	}
}

// TestUnknownPlatformKeepsSequence reproduces live run D009 on 2026-10-23: it
// departed Ipswich (static seq 1, platform 3, stop 600349) from stop 600350,
// which is in no static feed, at 06:29:32 (+32 s). The unknown stop cannot be
// shown to be at another station, so the observation stands: the call stays
// Ipswich, with its platform shown as unknown rather than as platform 3.
func TestUnknownPlatformKeepsSequence(t *testing.T) {
	ix := fixture(t)
	const d009 = "39765121-QR 26_27-44635-D009"
	now := brisbane(fri, 6, 35, 0)
	_, _, dep := sched(t, ix, d009, 1, fri)
	tu := newTU(t, ix, d009, "IPBR-5252", rtpb.TripDescriptor_REPLACEMENT, fri).
		event(1, "600350", dep.Add(32*time.Second), 32).delayed(32, 2)
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	c := callAt(t, leg(t, s, d009, fri), 1)
	if c.Name != "Ipswich station" || c.Platform != "" {
		t.Errorf("seq 1 rendered as %q platform %q; want Ipswich station, platform unknown", c.Name, c.Platform)
	}
	if c.Source != Observed || c.Delay() != 32*time.Second {
		t.Errorf("seq 1: source %v delay %v; want the departure observed at +32s", c.Source, c.Delay())
	}
}

// TestUpdateAtWrongStationIgnored reproduces live REPLACEMENT data for D510
// that reported Booval (stop 42) under sequence 22, which is Roma Street. A
// sequence match at a different station must not paste Booval's time onto
// Roma Street; the update still applies to Booval by station.
func TestUpdateAtWrongStationIgnored(t *testing.T) {
	ix := fixture(t)
	now := brisbane(fri, 6, 50, 0)
	booval, _, boovalDep := sched(t, ix, d510, 42, fri)
	tu := newTU(t, ix, d510, "NAIP-5252", rtpb.TripDescriptor_REPLACEMENT, fri).
		delayed(5, 19, 20, 21).
		event(22, booval, boovalDep.Add(10*time.Minute), 600)
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	l := leg(t, s, d510, fri)

	roma := callAt(t, l, 22)
	if roma.Name != "Roma Street station" || roma.Delay() != 5*time.Second || roma.Source != Projected {
		t.Errorf("Roma Street (seq 22): %s delay %v source %v; want +5s carried from Central, not Booval's +10m", roma.Name, roma.Delay(), roma.Source)
	}
	if b := callAt(t, l, 42); b.Source != Predicted || b.Delay() != 10*time.Minute {
		t.Errorf("Booval (seq 42): source %v delay %v; want the misnumbered update matched by station (+10m)", b.Source, b.Delay())
	}
}

// TestPostMidnightStartDateStamp reproduces a live observation: Translink
// stamps run 1RC6, scheduled at 24:02 on Thursday's service, with start_date
// 20261023 and start_time 00:02:00. Its realtime must still attach to
// Thursday's run, and must not attach to a run a week later.
func TestPostMidnightStartDateStamp(t *testing.T) {
	ix := fixture(t)
	const run1RC6 = "39764363-QR 26_27-44634-1RC6"
	now := brisbane(thu.AddDays(1), 0, 10, 0)

	live := newTU(t, ix, run1RC6, ix.Trips[run1RC6].RouteID, rtpb.TripDescriptor_SCHEDULED, thu).
		delayed(90, 5, 6, 7).stamped("20261023", "00:02:00")
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{live}), func() time.Time { return now })
	if c := callAt(t, leg(t, s, run1RC6, thu), 6); c.Source == Scheduled || c.Delay() != 90*time.Second {
		t.Errorf("calendar-stamped update not applied: seq 6 source %v delay %v; want +90s realtime", c.Source, c.Delay())
	}

	weekLater := newTU(t, ix, run1RC6, ix.Trips[run1RC6].RouteID, rtpb.TripDescriptor_SCHEDULED, thu).
		delayed(90, 5, 6, 7).stamped("20261030", "00:02:00")
	s = newService(ix, realtimeAt(t, now, []*tuBuilder{weekLater}), func() time.Time { return now })
	if c := callAt(t, leg(t, s, run1RC6, thu), 6); c.Source != Scheduled {
		t.Errorf("an update for a run a week later attached to Thursday's run: source %v", c.Source)
	}
}

func TestPunctualityBands(t *testing.T) {
	for _, tc := range []struct {
		delay int32
		want  Punctuality
	}{{-61, Early}, {-60, OnTime}, {0, OnTime}, {59, OnTime}, {60, Late}, {299, Late}, {300, VeryLate}} {
		if got := Classify(tc.delay); got != tc.want {
			t.Errorf("Classify(%d) = %v; want %v", tc.delay, got, tc.want)
		}
	}
}

func TestCancelledTripIsNotRideable(t *testing.T) {
	ix := fixture(t)
	now := brisbane(thu, 19, 50, 0)
	tu := newTU(t, ix, te95, "BNFG-5251", rtpb.TripDescriptor_CANCELED, thu)
	s := newService(ix, realtimeAt(t, now, []*tuBuilder{tu}), func() time.Time { return now })
	if l := leg(t, s, te95, thu); l.Punctuality != Cancelled {
		t.Errorf("cancelled trip punctuality %v", l.Punctuality)
	}
}

// TestRealCapture is the real-world regression (G-real): a live capture must
// reconcile with the static feed and merge without errors.
func TestRealCapture(t *testing.T) {
	ix := fixture(t)
	dirs, _ := filepath.Glob("../testdata/rt/*")
	if len(dirs) == 0 {
		t.Skip("no capture in testdata/rt")
	}
	dir := dirs[len(dirs)-1]
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	vehs, vpHeader, err := gtfsrt.DecodeVehicles(read("vp.pb"))
	if err != nil {
		t.Fatal(err)
	}
	// Decode trips relative to their own header so none are aged out.
	_, tuHeader, err := gtfsrt.DecodeTripUpdates(read("tu.pb"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	trips, _, err := gtfsrt.DecodeTripUpdates(read("tu.pb"), tuHeader)
	if err != nil {
		t.Fatal(err)
	}
	alerts, alHeader, err := gtfsrt.DecodeAlerts(read("alerts.pb"))
	if err != nil {
		t.Fatal(err)
	}

	resolved, total := 0, 0
	for id, tu := range trips {
		if tu.Rel == gtfsrt.RelAdded {
			continue
		}
		total++
		if ix.Trips[id] != nil {
			resolved++
		} else if tu.Rel == gtfsrt.RelReplacement {
			t.Errorf("REPLACEMENT trip %s does not resolve", id)
		}
	}
	if total == 0 || resolved*100 < total*85 {
		t.Fatalf("%d of %d realtime trips resolve; want ≥ 85%% (SPEC.md §2.3)", resolved, total)
	}

	now := tuHeader
	rt := &fakeRT{snap: gtfsrt.NewSnapshot(trips, tuHeader, vehs, vpHeader, alerts, alHeader)}
	s := newService(ix, rt, func() time.Time { return now })
	predicted := 0
	for id := range trips {
		if ix.Trips[id] == nil {
			continue
		}
		date, _ := ParseDate(trips[id].StartDate)
		l := leg(t, s, id, date)
		for _, c := range l.Calls {
			if c.Source == Predicted || c.Source == Observed {
				predicted++
			}
		}
	}
	if predicted == 0 {
		t.Error("no call carries realtime after merging the capture")
	}
	t.Logf("capture %s: %d/%d trips resolve, %d calls with realtime", filepath.Base(dir), resolved, total, predicted)
}
