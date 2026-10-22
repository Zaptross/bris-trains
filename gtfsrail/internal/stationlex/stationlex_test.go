package stationlex

import (
	"encoding/csv"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func loadStations(t *testing.T) []Entry {
	t.Helper()
	f, err := os.Open("../../../testdata/stations.csv")
	if err != nil {
		t.Fatalf("open stations: %v", err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read stations: %v", err)
	}
	var out []Entry
	for _, r := range rows[1:] {
		out = append(out, Entry{ID: r[0], Name: r[1]})
	}
	if len(out) != 154 {
		t.Fatalf("got %d stations, want 154", len(out))
	}
	return out
}

var interchanges = []string{
	"Bowen Hills station", "Roma Street station", "Central station", "Fortitude Valley station",
	"Eagle Junction station", "Northgate station", "Boggo Road station",
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Central Station":     "central",
		"  Kippa-Ring  ":      "kippa ring",
		"St. Peter’s":         "st peter s",
		"Café  Élan":          "cafe elan",
		"station":             "station",
		"Roma Street station": "roma street",
		"":                    "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	l := New(loadStations(t), DefaultAliases)
	for _, tc := range []struct {
		query     string
		want      string   // resolved Name
		ambiguous []string // candidate Names
	}{
		{query: "central", want: "Central station"},
		{query: "Central Station", want: "Central station"},
		{query: "roma", want: "Roma Street station"},
		{query: "roma st", want: "Roma Street station"},
		{query: "south", ambiguous: []string{"South Bank station", "South Brisbane station"}},
		{query: "airport", ambiguous: []string{"Domestic Airport station", "International Airport station"}},
		{query: "wynnum", want: "Wynnum station"},
		{query: "springfield", want: "Springfield station"},
		{query: "mango hill", want: "Mango Hill station"},
		{query: "romma", want: "Roma Street station"},
		{query: "indooroopily", want: "Indooroopilly station"},
		{query: "centrl", want: "Central station"},
		{query: "bowen hils", want: "Bowen Hills station"},
		{query: "nortgate", want: "Northgate station"},
		{query: "ipswitch", want: "Ipswich station"},
		{query: "cabolture", want: "Caboolture station"},
		{query: "eagle junc", want: "Eagle Junction station"},
		{query: "indro", want: "Indooroopilly station"},
		{query: "park road", want: "Boggo Road station"},
		{query: "kippa-ring", want: "Kippa-Ring station"},
		{query: "kippa ring", want: "Kippa-Ring station"},
		{query: "the valley", want: "Fortitude Valley station"},
		{query: "domestic", want: "Domestic Airport station"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got, err := l.Resolve(tc.query)
			if tc.ambiguous != nil {
				var amb *AmbiguousError
				if !errors.As(err, &amb) {
					t.Fatalf("Resolve(%q) = %v, %v; want AmbiguousError", tc.query, got, err)
				}
				if names := names(amb.Candidates); !slices.Equal(names, tc.ambiguous) {
					t.Fatalf("candidates = %v, want %v", names, tc.ambiguous)
				}
				return
			}
			if err != nil || got.Name != tc.want {
				t.Fatalf("Resolve(%q) = %v, %v; want %q", tc.query, got, err, tc.want)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	l := New(loadStations(t), DefaultAliases)

	_, err := l.Resolve("zzzzzz")
	var unk *UnknownError
	if !errors.As(err, &unk) {
		t.Fatalf("zzzzzz: got %v, want UnknownError", err)
	}
	if n := len(unk.Suggestions); n == 0 || n > 3 {
		t.Fatalf("zzzzzz: %d suggestions, want 1..3", n)
	}
	if !strings.HasPrefix(err.Error(), `unknown station "zzzzzz"; did you mean `) {
		t.Errorf("message = %q", err.Error())
	}

	for _, q := range []string{"", "  - "} {
		_, err := l.Resolve(q)
		if !errors.As(err, &unk) || len(unk.Suggestions) != 0 {
			t.Errorf("Resolve(%q) = %v, want UnknownError without suggestions", q, err)
		}
	}

	_, err = l.Resolve("south")
	if want := `ambiguous: "south" matches South Bank station, South Brisbane station`; err == nil || err.Error() != want {
		t.Errorf("message = %v, want %q", err, want)
	}
}

func TestUnknownErrorMessage(t *testing.T) {
	e := &UnknownError{Query: "xyz", Suggestions: []Entry{{Name: "A"}, {Name: "B"}, {Name: "C"}}}
	if got, want := e.Error(), `unknown station "xyz"; did you mean A, B or C?`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAliases(t *testing.T) {
	l := New(loadStations(t), DefaultAliases)
	if got := l.Aliases("place_romsta"); !slices.Contains(got, "roma st") {
		t.Errorf("Aliases(place_romsta) = %v, want to include roma st", got)
	}
	if got, want := l.Aliases("place_forsta"), []string{"fortitude", "the valley"}; !slices.Equal(got, want) {
		t.Errorf("Aliases(place_forsta) = %v, want %v", got, want)
	}
	if got := l.Aliases("no_such_id"); len(got) != 0 {
		t.Errorf("Aliases(no_such_id) = %v, want none", got)
	}
}

func TestValidate(t *testing.T) {
	stations := loadStations(t)
	reserved := []string{"serve"}

	if w := New(stations, DefaultAliases).Validate(interchanges, reserved); len(w) != 0 {
		t.Errorf("real list: unexpected warnings %q", w)
	}

	withServe := append(slices.Clone(stations), Entry{ID: "place_sersta", Name: "Serve station"})
	w := New(withServe, DefaultAliases).Validate(interchanges, reserved)
	if len(w) != 1 || !strings.Contains(w[0], "Serve station") || !strings.Contains(w[0], "reserved") {
		t.Errorf("Serve station: warnings %q, want one collision", w)
	}

	aliasServe := New(stations, map[string]string{"Serve": "Central station"}).Validate(nil, reserved)
	if len(aliasServe) != 1 || !strings.Contains(aliasServe[0], "reserved") {
		t.Errorf("alias serve: warnings %q, want one collision", aliasServe)
	}

	renamed := slices.Clone(stations)
	for i := range renamed {
		if renamed[i].Name == "Boggo Road station" {
			renamed[i].Name = "Ecosciences Precinct station"
		}
	}
	l := New(renamed, DefaultAliases)
	w = l.Validate(interchanges, reserved)
	if len(w) != 2 || !strings.Contains(w[0], `"park road"`) || !strings.Contains(w[1], `"Boggo Road station"`) {
		t.Errorf("renamed: warnings %q, want alias and required-name warnings", w)
	}
	if _, err := l.Resolve("park road"); err == nil {
		t.Errorf("renamed: park road still resolves")
	}
}
