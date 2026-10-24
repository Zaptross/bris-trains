//go:build live

package gtfs

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"maps"
	"os"
	"slices"
	"testing"
	"time"
)

// TestLiveFullFeed parses the full SEQ feed downloaded to scratch/ and checks
// what must hold for every edition of it. Translink republishes the feed
// roughly daily and trip counts move with each timetable change (9,919 rail
// trips on 21 Oct 2026, 10,606 two days later), so counts are only sanity
// floors; they are logged to make trends visible.
func TestLiveFullFeed(t *testing.T) {
	start := time.Now()
	ix, err := ParseFile("../../../scratch/SEQ_GTFS.zip")
	if err != nil {
		t.Fatal(err)
	}
	parse := time.Since(start)

	// Floors: about half of what the network ran in Oct 2026 (584 routes,
	// 9,919 trips, 248,021 stop times, 667 shapes). Falling below means a
	// parsing or filtering failure, not a timetable change.
	for _, c := range []struct {
		name       string
		got, floor int
	}{
		{"routes", len(ix.Routes), 250},
		{"trips", len(ix.Trips), 5000},
		{"stop times", len(ix.StopTimes), 120000},
		{"shapes", len(ix.Shapes), 300},
	} {
		if c.got < c.floor {
			t.Errorf("%s = %d, below the sanity floor %d", c.name, c.got, c.floor)
		}
	}

	// Structure: every platform belongs to a station, every station has a
	// platform, every trip's stops are in sequence order and resolvable.
	for id, s := range ix.Stops {
		if _, ok := ix.Stations[s.Parent]; !ok {
			t.Errorf("platform %s has parent %q, which is not a rail station", id, s.Parent)
		}
	}
	for id, st := range ix.Stations {
		if len(st.Platforms) == 0 {
			t.Errorf("station %s (%s) has no platforms", id, st.Name)
		}
	}
	used := map[string]bool{}
	for id, trip := range ix.Trips {
		sts := ix.TripStopTimes(trip)
		if len(sts) < 2 {
			t.Errorf("trip %s has %d stops", id, len(sts))
			continue
		}
		if !slices.IsSortedFunc(sts, func(a, b StopTime) int { return int(a.Seq) - int(b.Seq) }) {
			t.Errorf("trip %s stop times are not in sequence order", id)
		}
		for _, st := range sts {
			used[st.StopID] = true
			if _, ok := ix.StationOf(st.StopID); !ok {
				t.Errorf("trip %s calls at %s, which resolves to no station", id, st.StopID)
			}
		}
	}
	for id, pts := range ix.Shapes {
		if len(pts) > MaxShapePoints {
			t.Errorf("shape %s has %d points, over the %d budget", id, len(pts), MaxShapePoints)
		}
	}

	// Network drift: the rail station set is stable between timetable
	// changes, and a rename or new station must be noticed (Park Road became
	// Boggo Road without changing its ID). On a real change, regenerate the
	// list with `go run ./cmd/mkfixture` and review the diff.
	f, err := os.Open("../../../testdata/stations.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, r := range rows[1:] {
		want[r[0]] = r[1]
	}
	got := map[string]string{}
	for id, st := range ix.Stations {
		got[id] = st.Name
	}
	if !maps.Equal(got, want) {
		t.Errorf("rail stations changed from testdata/stations.csv:\n%s", stationDiff(want, got))
	}

	var buf bytes.Buffer
	start = time.Now()
	if err := ix.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	encode := time.Since(start)
	size := buf.Len()
	start = time.Now()
	if _, err := Decode(&buf); err != nil {
		t.Fatal(err)
	}
	decode := time.Since(start)
	t.Logf("feed %s–%s: %d routes, %d trips, %d stop times, %d stations, %d platforms used, %d shapes",
		ix.FeedStart, ix.FeedEnd, len(ix.Routes), len(ix.Trips), len(ix.StopTimes), len(ix.Stations), len(used), len(ix.Shapes))
	t.Logf("parse %v; gob %d bytes (%.2f MB), encode %v, decode %v",
		parse, size, float64(size)/(1<<20), encode, decode)
}

// stationDiff lists added, removed and renamed stations, sorted by ID.
func stationDiff(want, got map[string]string) string {
	var b bytes.Buffer
	ids := slices.Sorted(maps.Keys(want))
	for _, id := range slices.Sorted(maps.Keys(got)) {
		if _, ok := want[id]; !ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		w, inWant := want[id]
		g, inGot := got[id]
		switch {
		case !inGot:
			fmt.Fprintf(&b, "  removed %s %q\n", id, w)
		case !inWant:
			fmt.Fprintf(&b, "  added   %s %q\n", id, g)
		case w != g:
			fmt.Fprintf(&b, "  renamed %s %q -> %q\n", id, w, g)
		}
	}
	return b.String()
}
