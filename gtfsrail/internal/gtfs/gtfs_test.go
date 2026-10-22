package gtfs

import (
	"archive/zip"
	"bytes"
	"slices"
	"strings"
	"sync"
	"testing"
)

const fixturePath = "../../../testdata/static.zip"

var fixture = sync.OnceValues(func() (*Index, error) { return ParseFile(fixturePath) })

func loadFixture(t *testing.T) *Index {
	t.Helper()
	ix, err := fixture()
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return ix
}

func TestParseSecs(t *testing.T) {
	good := map[string]Secs{
		"26:16:00":   94560,
		"00:00:00":   0,
		"5:07:09":    5*3600 + 7*60 + 9,
		" 23:59:59 ": 86399,
		"24:00:00":   86400,
	}
	for in, want := range good {
		got, err := ParseSecs(in)
		if err != nil || got != want {
			t.Errorf("ParseSecs(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"12:60:00", "12:00:60", "12:00", "ab:00:00", "", "12:0:00", "12:00:0", ":00:00", "12:00:00:00", "-1:00:00", "12:+0:00", "1 2:00:00"} {
		if got, err := ParseSecs(in); err == nil {
			t.Errorf("ParseSecs(%q) = %d, want error", in, got)
		}
	}
}

func TestParseFixture(t *testing.T) {
	ix := loadFixture(t)
	if len(ix.Trips) != 1508 {
		t.Errorf("trips = %d, want 1508", len(ix.Trips))
	}
	if ix.FeedStart != "20261022" || ix.FeedEnd != "20261221" {
		t.Errorf("feed range = %s..%s", ix.FeedStart, ix.FeedEnd)
	}

	total, postMidnight := 0, false
	for id, tr := range ix.Trips {
		sts := ix.TripStopTimes(tr)
		if len(sts) < 2 {
			t.Fatalf("trip %s has %d stop times", id, len(sts))
		}
		total += len(sts)
		if _, ok := ix.Routes[tr.RouteID]; !ok {
			t.Errorf("trip %s: route %s not indexed", id, tr.RouteID)
		}
		if !slices.IsSortedFunc(sts, func(a, b StopTime) int { return int(a.Seq) - int(b.Seq) }) {
			t.Errorf("trip %s: stop times not sorted by Seq", id)
		}
		for _, st := range sts {
			if _, ok := ix.Stops[st.StopID]; !ok {
				t.Errorf("trip %s calls at unindexed stop %s", id, st.StopID)
			}
			if st.Departure >= 86400 {
				postMidnight = true
			}
		}
	}
	if total != len(ix.StopTimes) {
		t.Errorf("trip ranges cover %d of %d stop times", total, len(ix.StopTimes))
	}
	if !postMidnight {
		t.Error("no trip departs at or after 24:00:00")
	}

	for id, s := range ix.Stops {
		st, ok := ix.Stations[s.Parent]
		if !ok {
			t.Errorf("orphan platform %s (parent %q)", id, s.Parent)
			continue
		}
		if !slices.Contains(st.Platforms, id) {
			t.Errorf("station %s does not list platform %s", st.ID, id)
		}
	}
	for id, st := range ix.Stations {
		if !slices.IsSorted(st.Platforms) || len(st.Platforms) == 0 {
			t.Errorf("station %s platforms %v: want sorted, non-empty", id, st.Platforms)
		}
	}

	for _, id := range []string{"place_censta", "place_romsta"} {
		st, ok := ix.Stations[id]
		if !ok {
			t.Fatalf("station %s missing", id)
		}
		if len(st.Platforms) < 2 || st.Lat > -27 || st.Lat < -28 {
			t.Errorf("station %s = %+v", id, st)
		}
		for _, p := range st.Platforms {
			if got, ok := ix.StationOf(p); !ok || got != st {
				t.Errorf("StationOf(%s) = %v, %v; want %s", p, got, ok, id)
			}
		}
		if got, ok := ix.StationOf(id); !ok || got != st {
			t.Errorf("StationOf(%s) by station ID failed", id)
		}
	}
	if st := ix.Stations["place_censta"]; st.Name != "Central station" {
		t.Errorf("Central name = %q", st.Name)
	}
	if _, ok := ix.StationOf("no-such-stop"); ok {
		t.Error("StationOf(unknown) reported ok")
	}

	calling := ix.Calling["place_censta"]
	if len(calling) == 0 {
		t.Fatal("no trips call at Central")
	}
	if !slices.IsSorted(calling) || len(slices.Compact(slices.Clone(calling))) != len(calling) {
		t.Error("Calling[place_censta] not sorted and unique")
	}
	for _, id := range calling {
		if _, ok := ix.Trips[id]; !ok {
			t.Errorf("Calling lists unknown trip %s", id)
		}
	}

	r := ix.Routes["BDBR-5251"]
	if r == nil || r.Color != "FFC425" || r.TextColor != "1D1D1D" || r.LongName != "Airport - Brisbane City" {
		t.Errorf("route BDBR-5251 = %+v", r)
	}
	for id, pts := range ix.Shapes {
		if len(pts) > MaxShapePoints || len(pts) < 2 {
			t.Errorf("shape %s has %d points", id, len(pts))
		}
	}
	for _, tr := range ix.Trips {
		if _, ok := ix.Shapes[tr.ShapeID]; !ok {
			t.Errorf("trip %s shape %s missing", tr.ID, tr.ShapeID)
			break
		}
	}
}

func TestServiceActiveFixture(t *testing.T) {
	ix := loadFixture(t)
	cases := []struct {
		svc, date string
		want      bool
	}{
		{"QR 26_27-44634-0001000", "20261022", true},
		{"QR 26_27-44634-0001000", "20261023", false},
		{"QR 26_27-44635", "20261023", true},  // Friday
		{"QR 26_27-44635", "20261024", false}, // Saturday
		{"QR 26_27-44635", "20261030", false}, // Friday, removed by calendar_dates
		{"QR 26_27-44635", "20261113", true},  // last day of range
		{"QR 26_27-44635", "20261120", false}, // Friday after range
		{"QR 26_27-44635", "not-a-date", false},
		{"no-such-service", "20261023", false},
	}
	for _, c := range cases {
		if got := ix.ServiceActive(c.svc, c.date); got != c.want {
			t.Errorf("ServiceActive(%q, %s) = %v, want %v", c.svc, c.date, got, c.want)
		}
	}
}

func TestServiceActiveOverrides(t *testing.T) {
	weekdays := Calendar{Days: [7]bool{false, true, true, true, true, true, false}, Start: "20261001", End: "20261031"}
	ix := &Index{
		Calendars: map[string]Calendar{"wk": weekdays},
		CalendarDates: map[string]map[string]uint8{
			"wk":    {"20261023": 2, "20261024": 1},
			"extra": {"20261225": 1},
		},
	}
	cases := []struct {
		svc, date string
		want      bool
	}{
		{"wk", "20261022", true},  // Thursday in range
		{"wk", "20261023", false}, // Friday, removed
		{"wk", "20261024", true},  // Saturday, added
		{"wk", "20261025", false}, // Sunday
		{"wk", "20261102", false}, // Monday after range
		{"extra", "20261225", true},
		{"extra", "20261226", false},
	}
	for _, c := range cases {
		if got := ix.ServiceActive(c.svc, c.date); got != c.want {
			t.Errorf("ServiceActive(%q, %s) = %v, want %v", c.svc, c.date, got, c.want)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ix := loadFixture(t)
	var buf bytes.Buffer
	if err := ix.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Routes) != len(ix.Routes) || len(got.Stops) != len(ix.Stops) || len(got.Stations) != len(ix.Stations) ||
		len(got.Trips) != len(ix.Trips) || len(got.StopTimes) != len(ix.StopTimes) || len(got.Shapes) != len(ix.Shapes) ||
		len(got.Calendars) != len(ix.Calendars) || len(got.CalendarDates) != len(ix.CalendarDates) || len(got.Calling) != len(ix.Calling) {
		t.Fatal("decoded counts differ")
	}
	for _, id := range ix.Calling["place_censta"][:3] {
		if !slices.Equal(got.TripStopTimes(got.Trips[id]), ix.TripStopTimes(ix.Trips[id])) {
			t.Errorf("trip %s stop times differ after round trip", id)
		}
		if *got.Trips[id] != *ix.Trips[id] {
			t.Errorf("trip %s differs: %+v vs %+v", id, got.Trips[id], ix.Trips[id])
		}
	}
	if !slices.Equal(got.Stations["place_romsta"].Platforms, ix.Stations["place_romsta"].Platforms) {
		t.Error("Roma Street platforms differ")
	}
	for sid, trips := range ix.Calling {
		if !slices.Equal(got.Calling[sid], trips) {
			t.Errorf("Calling[%s] differs after round trip", sid)
		}
	}
	if got.ServiceActive("QR 26_27-44635", "20261030") {
		t.Error("calendar_dates removal lost in round trip")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("not a gob"), {0x05, 0xff, 0x81, 0x03, 0x01}} {
		if _, err := Decode(bytes.NewReader(in)); err == nil {
			t.Errorf("Decode(%q) succeeded", in)
		}
	}
	// A truncated valid stream must fail, not return a partial index.
	var buf bytes.Buffer
	if err := loadFixture(t).Encode(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(buf.Bytes()[:buf.Len()/2])); err == nil {
		t.Error("Decode of truncated stream succeeded")
	}
}

// zipOf builds an in-memory zip of the given files.
func zipOf(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func crlf(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

// miniFeed is a hand-written feed exercising the CSV quirks: BOM, CRLF,
// quoted fields, padded values, reordered columns, and bad rows.
func miniFeed() map[string]string {
	return map[string]string{
		"routes.txt": crlf(
			"\ufeffroute_type,route_id,route_long_name,route_short_name,route_color,route_text_color",
			`2,R1,"Ferny Grove - Beenleigh, via City",BNFG,0053A0,FFFFFF`,
			`3,B1,"Bus",B1,000000,FFFFFF`,
		),
		"trips.txt": crlf(
			"trip_id,route_id,service_id,trip_headsign,direction_id,shape_id",
			"T1,R1,S1,City,1,SH1",
			"T2,R1,S1,Home,0,SH1",
			"X1,B1,S9,Bus,0,SHB",
		),
		"stops.txt": crlf(
			"stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station,platform_code",
			`place_a,"A station"," -27.5"," 153.0",1,,`,
			`pa1,"A station, platform 1", -27.5, 153.0,0,place_a,1`,
			`pa2,"A station, platform 2", -27.5, 153.0,0,place_a,2`,
			`busa,"Rd at A station", -27.5, 153.0,0,place_a,`,
			`place_b,B station,-27.6,153.1,1,,`,
			`pb1,B station platform 1,-27.6,153.1,0,place_b,1`,
			`bad,Bad,not-a-number,153,0,place_b,`,
			`place_c,C bus station,-27.7,153.2,1,,`,
			`pc1,C bus stop,-27.7,153.2,0,place_c,`,
		),
		"stop_times.txt": crlf(
			"stop_sequence,trip_id,arrival_time,departure_time,stop_id,pickup_type,drop_off_type",
			"2,T1,23:58:00,24:01:00,pb1,0,0",
			"1,T1, 23:50:00 , 23:50:30 ,pa1,0,1",
			"3,T1,24:10:00,24:10:00,pa2,1,0",
			"1,T2,08:00:00,08:00:00,pb1,,",
			"2,T2,8:61:00,08:10:00,pa1,0,0",
			"3,T2,08:20:00,08:20:00,pa1,0,0",
			"1,X1,08:00:00,08:00:00,busa,0,0",
			"2,X1,08:05:00,08:05:00,pc1,0,0",
		),
		"shapes.txt": crlf(
			"shape_id,shape_pt_sequence,shape_pt_lat,shape_pt_lon",
			"SH1,3,-27.7,153.2",
			"SH1,1,-27.5,153.0",
			"SH1,2,-27.6,153.1",
			"SHB,1,0,0",
			"SHB,2,1,1",
		),
		"calendar.txt": crlf(
			"service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date",
			"S1,1,1,1,1,1,0,0,20261001,20261231",
			"S9,1,1,1,1,1,1,1,20261001,20261231",
		),
		"calendar_dates.txt": crlf(
			"service_id,date,exception_type",
			"S1,20261023,2",
			"S9,20261023,2",
		),
	}
}

func TestParseCSVQuirks(t *testing.T) {
	r := zipOf(t, miniFeed())
	ix, err := Parse(r, r.Size())
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Routes) != 1 || ix.Routes["R1"].LongName != "Ferny Grove - Beenleigh, via City" || ix.Routes["R1"].Color != "0053A0" {
		t.Errorf("routes = %+v", ix.Routes)
	}
	if len(ix.Trips) != 2 || ix.Trips["T1"].Direction != 1 || ix.Trips["T1"].Headsign != "City" {
		t.Errorf("trips = %+v", ix.Trips)
	}

	t1 := ix.TripStopTimes(ix.Trips["T1"])
	want := []StopTime{
		{StopID: "pa1", Seq: 1, Arrival: 85800, Departure: 85830, DropOffType: 1},
		{StopID: "pb1", Seq: 2, Arrival: 86280, Departure: 86460},
		{StopID: "pa2", Seq: 3, Arrival: 87000, Departure: 87000, PickupType: 1},
	}
	if !slices.Equal(t1, want) {
		t.Errorf("T1 stop times = %+v\nwant %+v", t1, want)
	}
	// T2's malformed row (8:61:00) is skipped, not fatal.
	if t2 := ix.TripStopTimes(ix.Trips["T2"]); len(t2) != 2 || t2[0].Seq != 1 || t2[1].Seq != 3 {
		t.Errorf("T2 stop times = %+v", t2)
	}

	if len(ix.Stations) != 2 || ix.Stations["place_a"] == nil || ix.Stations["place_b"] == nil {
		t.Fatalf("stations = %v", ix.Stations)
	}
	if a := ix.Stations["place_a"]; a.Lat != -27.5 || a.Lon != 153.0 || !slices.Equal(a.Platforms, []string{"pa1", "pa2"}) {
		t.Errorf("place_a = %+v", a)
	}
	// busa is served by a bus trip; pc1's parent is not a rail station.
	if _, ok := ix.Stops["busa"]; ok {
		t.Error("bus stop busa indexed as a platform")
	}
	if _, ok := ix.Stops["pc1"]; ok {
		t.Error("pc1 indexed")
	}
	if !slices.Equal(ix.Calling["place_a"], []string{"T1", "T2"}) || !slices.Equal(ix.Calling["place_b"], []string{"T1", "T2"}) {
		t.Errorf("calling = %v", ix.Calling)
	}

	if len(ix.Shapes) != 1 || len(ix.Shapes["SH1"]) != 3 || ix.Shapes["SH1"][0].Lat != -27.5 || ix.Shapes["SH1"][2].Lat != -27.7 {
		t.Errorf("shapes = %v", ix.Shapes)
	}
	if _, ok := ix.Calendars["S9"]; ok {
		t.Error("non-rail calendar retained")
	}
	if !ix.ServiceActive("S1", "20261022") || ix.ServiceActive("S1", "20261023") {
		t.Error("S1 calendar wrong")
	}
	if ix.FeedStart != "" {
		t.Errorf("FeedStart = %q without feed_info.txt", ix.FeedStart)
	}
}

func TestParseRejectsStructuralErrors(t *testing.T) {
	missingCol := miniFeed()
	missingCol["stop_times.txt"] = crlf("trip_id,stop_id,arrival_time,departure_time", "T1,pa1,08:00:00,08:00:00")
	missingFile := miniFeed()
	delete(missingFile, "trips.txt")
	for name, files := range map[string]map[string]string{"missing column": missingCol, "missing file": missingFile} {
		r := zipOf(t, files)
		if _, err := Parse(r, r.Size()); err == nil {
			t.Errorf("%s: Parse succeeded", name)
		}
	}
	if _, err := Parse(strings.NewReader("not a zip"), 9); err == nil {
		t.Error("Parse of non-zip succeeded")
	}
}
