//go:build live

package gtfs

import (
	"bytes"
	"encoding/csv"
	"maps"
	"os"
	"testing"
	"time"
)

// TestLiveFullFeed parses the full SEQ feed downloaded to scratch/ and checks
// it against the counts measured in SPEC §2.1.
func TestLiveFullFeed(t *testing.T) {
	start := time.Now()
	ix, err := ParseFile("../../../scratch/SEQ_GTFS.zip")
	if err != nil {
		t.Fatal(err)
	}
	parse := time.Since(start)

	used := map[string]bool{}
	for _, st := range ix.StopTimes {
		used[st.StopID] = true
	}
	maxPts := 0
	for _, pts := range ix.Shapes {
		maxPts = max(maxPts, len(pts))
	}
	checks := []struct {
		name      string
		got, want int
	}{
		{"routes", len(ix.Routes), 584},
		{"trips", len(ix.Trips), 9919},
		{"stop times", len(ix.StopTimes), 248021},
		{"stations", len(ix.Stations), 154},
		{"platforms used by rail trips", len(used), 359},
		{"shapes", len(ix.Shapes), 667},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if maxPts > MaxShapePoints {
		t.Errorf("largest shape has %d points", maxPts)
	}

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
		t.Errorf("station set differs from testdata/stations.csv (%d vs %d)", len(got), len(want))
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
	t.Logf("parse %v; stops %d; gob %d bytes (%.2f MB), encode %v, decode %v",
		parse, len(ix.Stops), size, float64(size)/(1<<20), encode, decode)
}
