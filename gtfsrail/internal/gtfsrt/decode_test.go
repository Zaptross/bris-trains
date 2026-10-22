package gtfsrt

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

const captureDir = "../../../testdata/rt/20261023T000334"

func readCapture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(captureDir, name))
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return b
}

func marshal(t *testing.T, ents []*gtfs.FeedEntity, header int64) []byte {
	t.Helper()
	m := &gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Timestamp: proto.Uint64(uint64(header))},
		Entity: ents,
	}
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestDecodeCapture(t *testing.T) {
	tuBytes := readCapture(t, "tu.pb")
	_, header, err := DecodeTripUpdates(tuBytes, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if header.IsZero() {
		t.Fatal("TU header timestamp missing")
	}
	trips, _, err := DecodeTripUpdates(tuBytes, header)
	if err != nil {
		t.Fatal(err)
	}
	if len(trips) == 0 {
		t.Fatal("no trips decoded")
	}
	rels := map[TripRel]int{}
	for id, tu := range trips {
		if tu.TripID != id {
			t.Errorf("trip keyed %q has TripID %q", id, tu.TripID)
		}
		rels[tu.Rel]++
	}

	vehicles, vpHeader, err := DecodeVehicles(readCapture(t, "vp.pb"))
	if err != nil {
		t.Fatal(err)
	}
	stale := 0
	for _, v := range vehicles {
		if v.TripID == "" {
			t.Errorf("vehicle %q has no TripID", v.ID)
		}
		if v.Stale {
			stale++
		}
	}

	alerts, alHeader, err := DecodeAlerts(readCapture(t, "alerts.pb"))
	if err != nil {
		t.Fatal(err)
	}
	withRoutes := 0
	for _, a := range alerts {
		if len(a.Routes) > 0 {
			withRoutes++
		}
	}
	if withRoutes == 0 {
		t.Error("no alert has informed routes")
	}
	snap := NewSnapshot(trips, header, vehicles, vpHeader, alerts, alHeader)
	t.Logf("TU header %s: trips=%d rels{sched=%d added=%d unsched=%d canceled=%d repl=%d dup=%d deleted=%d}",
		header.In(brisbane).Format(time.RFC3339), len(trips), rels[RelScheduled], rels[RelAdded], rels[RelUnscheduled],
		rels[RelCanceled], rels[RelReplacement], rels[RelDuplicated], rels[RelDeleted])
	t.Logf("VP header %s: vehicles=%d stale=%d", vpHeader.In(brisbane).Format(time.RFC3339), len(vehicles), stale)
	t.Logf("alerts=%d withRoutes=%d indexedRoutes=%d indexedTrips=%d indexedStops=%d",
		len(alerts), withRoutes, len(snap.AlertsByRoute), len(snap.AlertsByTrip), len(snap.AlertsByStop))
}

func TestDecodeMalformed(t *testing.T) {
	bad := []byte{0x0a, 0xff, 0xff, 0xff}
	if _, _, err := DecodeTripUpdates(bad, time.Now()); err == nil {
		t.Error("trip updates: want error")
	}
	if _, _, err := DecodeVehicles(bad); err == nil {
		t.Error("vehicles: want error")
	}
	if _, _, err := DecodeAlerts(bad); err == nil {
		t.Error("alerts: want error")
	}
}

func vehicleEntity(trip string, ts int64) *gtfs.FeedEntity {
	vp := &gtfs.VehiclePosition{
		Trip:                &gtfs.TripDescriptor{TripId: proto.String(trip)},
		Vehicle:             &gtfs.VehicleDescriptor{Id: proto.String("v-" + trip), Label: proto.String("L" + trip)},
		Position:            &gtfs.Position{Latitude: proto.Float32(-27.5), Longitude: proto.Float32(153)},
		CurrentStatus:       gtfs.VehiclePosition_STOPPED_AT.Enum(),
		CurrentStopSequence: proto.Uint32(4),
		StopId:              proto.String("600001"),
	}
	if ts != 0 {
		vp.Timestamp = proto.Uint64(uint64(ts))
	}
	return &gtfs.FeedEntity{Id: proto.String(trip), Vehicle: vp}
}

func TestVehicleStaleness(t *testing.T) {
	const h = 1_800_000_000
	b := marshal(t, []*gtfs.FeedEntity{
		vehicleEntity("fresh", h-120),
		vehicleEntity("stale", h-121),
		vehicleEntity("nots", 0),
		{Id: proto.String("notrip"), Vehicle: &gtfs.VehiclePosition{Timestamp: proto.Uint64(h)}},
	}, h)
	vs, header, err := DecodeVehicles(b)
	if err != nil {
		t.Fatal(err)
	}
	if !header.Equal(time.Unix(h, 0)) {
		t.Errorf("header = %v", header)
	}
	if len(vs) != 3 {
		t.Fatalf("got %d vehicles, want 3 (trip-less dropped)", len(vs))
	}
	for id, want := range map[string]bool{"fresh": false, "stale": true, "nots": true} {
		if got := vs[id].Stale; got != want {
			t.Errorf("%s: Stale = %v, want %v", id, got, want)
		}
	}
	v := vs["fresh"]
	if v.Status != StatusStoppedAt || v.Status.String() != "STOPPED_AT" || v.Seq != 4 || v.StopID != "600001" ||
		v.ID != "v-fresh" || v.Label != "Lfresh" || v.Lat != float64(float32(-27.5)) || v.Lon != 153 {
		t.Errorf("fields: %+v", v)
	}
	if !vs["nots"].Timestamp.IsZero() {
		t.Error("absent timestamp should be zero")
	}
}

func stu(seq uint32, arr int64) *gtfs.TripUpdate_StopTimeUpdate {
	return &gtfs.TripUpdate_StopTimeUpdate{
		StopSequence: proto.Uint32(seq),
		StopId:       proto.String("s"),
		Arrival:      &gtfs.TripUpdate_StopTimeEvent{Time: proto.Int64(arr), Delay: proto.Int32(30)},
	}
}

func tripEntity(id string, stus ...*gtfs.TripUpdate_StopTimeUpdate) *gtfs.FeedEntity {
	return &gtfs.FeedEntity{Id: proto.String(id), TripUpdate: &gtfs.TripUpdate{
		Trip:           &gtfs.TripDescriptor{TripId: proto.String(id)},
		StopTimeUpdate: stus,
	}}
}

func TestTripUpdateDropBoundary(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	n := now.Unix()
	b := marshal(t, []*gtfs.FeedEntity{
		tripEntity("kept", stu(1, n-7200), stu(2, n-3600)),
		tripEntity("dropped", stu(1, n-7200), stu(2, n-3601)),
		tripEntity("notimes", &gtfs.TripUpdate_StopTimeUpdate{StopSequence: proto.Uint32(1),
			ScheduleRelationship: gtfs.TripUpdate_StopTimeUpdate_NO_DATA.Enum()}),
		tripEntity("empty"),
	}, n)
	trips, _, err := DecodeTripUpdates(b, now)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"kept": true, "dropped": false, "notimes": true, "empty": true} {
		if _, got := trips[id]; got != want {
			t.Errorf("%s present = %v, want %v", id, got, want)
		}
	}
	if !trips["notimes"].Updates[0].NoData {
		t.Error("NO_DATA not mapped")
	}
}

func TestTripUpdateFields(t *testing.T) {
	n := int64(1_800_000_000)
	skipped := stu(7, n)
	skipped.ScheduleRelationship = gtfs.TripUpdate_StopTimeUpdate_SKIPPED.Enum()
	assigned := stu(3, n)
	assigned.StopTimeProperties = &gtfs.TripUpdate_StopTimeUpdate_StopTimeProperties{AssignedStopId: proto.String("600029")}
	assigned.Departure = &gtfs.TripUpdate_StopTimeEvent{Delay: proto.Int32(-5)}
	ent := tripEntity("t1", skipped, assigned, stu(5, n))
	ent.TripUpdate.Trip.RouteId = proto.String("BRCL-4596")
	ent.TripUpdate.Trip.StartDate = proto.String("20261023")
	ent.TripUpdate.Trip.StartTime = proto.String("25:10:00")
	ent.TripUpdate.Trip.ScheduleRelationship = gtfs.TripDescriptor_REPLACEMENT.Enum()
	ent.TripUpdate.Vehicle = &gtfs.VehicleDescriptor{Id: proto.String("V1"), Label: proto.String("IMU")}
	trips, _, err := DecodeTripUpdates(marshal(t, []*gtfs.FeedEntity{ent}, n), time.Unix(n, 0))
	if err != nil {
		t.Fatal(err)
	}
	tu := trips["t1"]
	if tu.RouteID != "BRCL-4596" || tu.StartDate != "20261023" || tu.StartTime != "25:10:00" ||
		tu.Rel != RelReplacement || tu.VehicleID != "V1" || tu.VehicleLabel != "IMU" {
		t.Errorf("trip fields: %+v", tu)
	}
	if len(tu.Updates) != 3 || tu.Updates[0].Seq != 3 || tu.Updates[1].Seq != 5 || tu.Updates[2].Seq != 7 {
		t.Fatalf("updates not sorted by seq: %+v", tu.Updates)
	}
	u := tu.Updates[0]
	if u.AssignedStopID != "600029" || !u.HasArrival || !u.Arrival.HasTime || u.Arrival.Time != n ||
		!u.Arrival.HasDelay || u.Arrival.Delay != 30 || !u.HasDeparture || u.Departure.HasTime || u.Departure.Delay != -5 {
		t.Errorf("stu fields: %+v", u)
	}
	if !tu.Updates[2].Skipped || tu.Updates[1].Skipped {
		t.Error("SKIPPED not mapped")
	}
}

func TestDecodeAlertsSynthetic(t *testing.T) {
	ts := func(lang, text string) *gtfs.TranslatedString_Translation {
		tr := &gtfs.TranslatedString_Translation{Text: proto.String(text)}
		if lang != "" {
			tr.Language = proto.String(lang)
		}
		return tr
	}
	a := &gtfs.Alert{
		ActivePeriod: []*gtfs.TimeRange{{Start: proto.Uint64(100)}, {End: proto.Uint64(200)}},
		InformedEntity: []*gtfs.EntitySelector{
			{RouteId: proto.String("R1")}, {RouteId: proto.String("R1")},
			{Trip: &gtfs.TripDescriptor{TripId: proto.String("T1")}},
			{StopId: proto.String("S1"), RouteType: proto.Int32(2)},
		},
		Cause:           gtfs.Alert_MAINTENANCE.Enum(),
		Effect:          gtfs.Alert_REDUCED_SERVICE.Enum(),
		HeaderText:      &gtfs.TranslatedString{Translation: []*gtfs.TranslatedString_Translation{ts("fr", "bonjour"), ts("en", "hello")}},
		DescriptionText: &gtfs.TranslatedString{Translation: []*gtfs.TranslatedString_Translation{ts("", "desc")}},
	}
	alerts, _, err := DecodeAlerts(marshal(t, []*gtfs.FeedEntity{{Id: proto.String("A1"), Alert: a}}, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts", len(alerts))
	}
	got := alerts[0]
	if got.ID != "A1" || got.Header != "hello" || got.Description != "desc" ||
		got.Cause != "MAINTENANCE" || got.Effect != "REDUCED_SERVICE" {
		t.Errorf("fields: %+v", got)
	}
	if len(got.Routes) != 1 || got.Routes[0] != "R1" || len(got.Trips) != 1 || got.Trips[0] != "T1" ||
		len(got.Stops) != 1 || got.Stops[0] != "S1" {
		t.Errorf("entities: %+v", got)
	}
	want := []Period{{Start: time.Unix(100, 0)}, {End: time.Unix(200, 0)}}
	if len(got.Periods) != 2 || !got.Periods[0].Start.Equal(want[0].Start) || !got.Periods[0].End.IsZero() ||
		!got.Periods[1].Start.IsZero() || !got.Periods[1].End.Equal(want[1].End) {
		t.Errorf("periods: %+v", got.Periods)
	}
	snap := NewSnapshot(nil, time.Time{}, nil, time.Time{}, alerts, time.Time{})
	if len(snap.AlertsByRoute["R1"]) != 1 || len(snap.AlertsByTrip["T1"]) != 1 || len(snap.AlertsByStop["S1"]) != 1 {
		t.Errorf("indexes: %+v %+v %+v", snap.AlertsByRoute, snap.AlertsByTrip, snap.AlertsByStop)
	}
}

func TestAlertActiveAt(t *testing.T) {
	// Nine nightly closures 20:00–04:00 Brisbane, as in the real multi-window track-work alerts.
	base := time.Date(2026, 10, 23, 20, 0, 0, 0, brisbane)
	var nine Alert
	for d := range 9 {
		s := base.AddDate(0, 0, d)
		nine.Periods = append(nine.Periods, Period{Start: s, End: s.Add(8 * time.Hour)})
	}
	third := nine.Periods[2]
	cases := []struct {
		name   string
		a      *Alert
		at     time.Time
		active bool
		until  time.Time
	}{
		{"before first", &nine, base.Add(-time.Second), false, time.Time{}},
		{"at first start", &nine, base, true, nine.Periods[0].End},
		{"inside third", &nine, third.Start.Add(3 * time.Hour), true, third.End},
		{"at third end", &nine, third.End, false, time.Time{}},
		{"between windows", &nine, third.End.Add(time.Hour), false, time.Time{}},
		{"last second of ninth", &nine, nine.Periods[8].End.Add(-time.Second), true, nine.Periods[8].End},
		{"after ninth", &nine, nine.Periods[8].End.Add(time.Hour), false, time.Time{}},
		{"no periods", &Alert{}, base, true, time.Time{}},
		{"open-ended before", &Alert{Periods: []Period{{Start: base}}}, base.Add(-time.Second), false, time.Time{}},
		{"open-ended after", &Alert{Periods: []Period{{Start: base}}}, base.AddDate(1, 0, 0), true, time.Time{}},
		{"overlap latest end", &Alert{Periods: []Period{{Start: base, End: base.Add(time.Hour)}, {End: base.Add(2 * time.Hour)}}},
			base.Add(time.Minute), true, base.Add(2 * time.Hour)},
		{"overlap open wins", &Alert{Periods: []Period{{Start: base, End: base.Add(time.Hour)}, {Start: base}}},
			base.Add(time.Minute), true, time.Time{}},
	}
	for _, c := range cases {
		active, until := c.a.ActiveAt(c.at)
		if active != c.active || !until.Equal(c.until) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, active, until, c.active, c.until)
		}
	}
}
