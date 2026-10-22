// Package query implements the default `trains <origin> <destination>` mode:
// resolve both stations, fetch realtime once, print the next journeys, exit.
package query

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"trains/gtfsrail"
	"trains/gtfsrail/wire"
)

// Usage lists both modes; it is shown for `trains`, `trains --help` and usage
// errors.
const Usage = `usage:
  trains <origin> <destination> [query flags]
  trains serve [serve flags]

Multi-word stations are quoted: trains "ferny grove" cleveland

query flags:
  -n, --count int     journeys to show (default 5)
      --at string     query time, RFC3339 or HH:MM (default now)
      --direct        suppress one-change options
      --json          emit JSON instead of a table
      --no-color      disable ANSI colour
      --cache string  index cache dir (default <user cache dir>/trains)

serve flags: see trains serve --help
`

// FlagNames lists query-mode flag names without dashes, for serve mode's
// wrong-mode hint.
func FlagNames() []string { return []string{"n", "count", "at", "direct", "json", "no-color", "cache"} }

type options struct {
	count    int
	at       string
	direct   bool
	json     bool
	noColor  bool
	cache    string
	stations []string
}

// errHelp means --help was given.
var errHelp = errors.New("help requested")

// usageError is a command-line mistake, reported with exit status 2.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// parseArgs parses flags anywhere among the positional arguments. Go's flag
// package stops at the first non-flag, so parsing resumes after each
// positional (SPEC.md §4.1).
func parseArgs(args, serveFlags []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("trains", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&o.count, "n", 5, "")
	fs.IntVar(&o.count, "count", 5, "")
	fs.StringVar(&o.at, "at", "", "")
	fs.BoolVar(&o.direct, "direct", false, "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	fs.StringVar(&o.cache, "cache", "", "")

	for _, a := range args {
		name := flagName(a)
		if name == "" || fs.Lookup(name) != nil || name == "h" || name == "help" {
			continue
		}
		if slices.Contains(serveFlags, name) {
			return o, &usageError{fmt.Sprintf("--%s is a serve flag: trains serve --%s …", name, name)}
		}
	}

	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return o, errHelp
			}
			return o, &usageError{err.Error()}
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		o.stations = append(o.stations, rest[0])
		rest = rest[1:]
	}
	if len(o.stations) != 2 {
		return o, &usageError{fmt.Sprintf("expected an origin and a destination, got %d argument(s); quote multi-word stations: trains \"ferny grove\" cleveland", len(o.stations))}
	}
	if o.count < 1 || o.count > 20 {
		return o, &usageError{"--count must be between 1 and 20"}
	}
	return o, nil
}

// flagName returns the flag name in "-x", "--x" or "--x=v", else "".
func flagName(a string) string {
	if len(a) < 2 || a[0] != '-' || a == "--" {
		return ""
	}
	a = strings.TrimLeft(a, "-")
	if i := strings.IndexByte(a, '='); i >= 0 {
		a = a[:i]
	}
	return a
}

// Run executes query mode. Exit status: 0 success; 1 realtime unavailable (the
// schedule is still printed) or timetable unavailable; 2 usage or station
// errors.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, serveFlags []string) int {
	o, err := parseArgs(args, serveFlags)
	if errors.Is(err, errHelp) {
		fmt.Fprint(stdout, Usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n\n%s", err, Usage)
		return 2
	}
	if o.cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			fmt.Fprintf(stderr, "trains: no cache directory: %v (pass --cache)\n", err)
			return 1
		}
		o.cache = filepath.Join(dir, "trains")
	}
	at, err := parseAt(o.at, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n", err)
		return 2
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc, err := gtfsrail.Open(ctx, gtfsrail.Config{
		CacheDir: o.cache, ReservedWords: []string{"serve"}, Logger: logger,
	})
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n", err)
		return 1
	}
	defer svc.Close()

	origin, err := resolve(svc, o.stations[0])
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n", err)
		return 2
	}
	dest, err := resolve(svc, o.stations[1])
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n", err)
		return 2
	}

	journeys, fresh, err := svc.Journeys(ctx, origin.ID, dest.ID, at, gtfsrail.JourneyOptions{Limit: o.count, DirectOnly: o.direct})
	if errors.Is(err, gtfsrail.ErrSameStation) {
		fmt.Fprintf(stderr, "trains: %s is both the origin and the destination\n", origin.Name)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "trains: %v\n", err)
		return 1
	}

	if o.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(wire.FromJourneys(origin, dest, journeys, fresh)); err != nil {
			fmt.Fprintf(stderr, "trains: %v\n", err)
			return 1
		}
	} else {
		color := !o.noColor && os.Getenv("NO_COLOR") == "" && terminalColor(stdout)
		render(stdout, origin, dest, journeys, fresh, at, color)
	}
	if fresh.Degraded {
		return 1
	}
	return 0
}

func resolve(svc *gtfsrail.Service, q string) (gtfsrail.Station, error) {
	if st, ok := svc.Station(gtfsrail.StationID(q)); ok {
		return st, nil
	}
	return svc.Resolve(q)
}

// parseAt accepts "", RFC3339, or HH:MM (today in Brisbane).
func parseAt(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("15:04", s, gtfsrail.Brisbane)
	if err != nil {
		return time.Time{}, fmt.Errorf("--at %q: want RFC3339 or HH:MM", s)
	}
	y, m, d := now.In(gtfsrail.Brisbane).Date()
	return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, gtfsrail.Brisbane), nil
}
