package query

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFlagsMayAppearAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"central", "roma", "--json", "-n", "3"},
		{"--json", "central", "roma", "-n", "3"},
		{"central", "--json", "roma", "-n", "3"},
		{"-n=3", "central", "--json", "roma"},
	} {
		o, err := parseArgs(args, nil)
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if !slices.Equal(o.stations, []string{"central", "roma"}) || !o.json || o.count != 3 {
			t.Errorf("%v parsed as %+v", args, o)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"ferny", "grove", "cleveland"}, `quote multi-word stations`},
		{[]string{"central"}, `got 1 argument`},
		{[]string{"central", "roma", "--addr", ":8080"}, `--addr is a serve flag`},
		{[]string{"central", "roma", "--bogus"}, `flag provided but not defined`},
		{[]string{"central", "roma", "-n", "0"}, `between 1 and 20`},
	} {
		_, err := parseArgs(tc.args, []string{"addr", "dev", "cache"})
		var ue *usageError
		if !errors.As(err, &ue) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: error %v; want usage error containing %q", tc.args, err, tc.want)
		}
	}
	if _, err := parseArgs([]string{"--help"}, nil); !errors.Is(err, errHelp) {
		t.Errorf("--help: %v", err)
	}
}

func TestParseAt(t *testing.T) {
	now := time.Date(2026, 10, 23, 0, 30, 0, 0, time.UTC)
	got, err := parseAt("08:15", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format(time.RFC3339) != "2026-10-23T08:15:00+10:00" {
		t.Errorf("HH:MM resolved to %s; want today in Brisbane", got.Format(time.RFC3339))
	}
	if _, err := parseAt("8am", now); err == nil {
		t.Error("8am accepted")
	}
}
