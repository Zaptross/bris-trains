// Package gtfsrail answers "when is the next train from A to B, and where is
// it now" for Translink's South East Queensland rail network.
//
// It merges the static GTFS schedule with the GTFS-Realtime trip update,
// vehicle position and alert feeds. The static schedule is always the spine;
// realtime data overlays it where available (see SPEC.md §3.5).
package gtfsrail

import (
	"fmt"
	"time"
)

// Identifier types. They are distinct so a platform can never be passed where
// a station is expected.
type (
	StopID    string // a platform (GTFS location_type 0)
	StationID string // a parent station (GTFS location_type 1), e.g. "place_censta"
	TripID    string
	RouteID   string
	ServiceID string
	ShapeID   string
)

// Date is a service date in Australia/Brisbane.
type Date struct {
	Year  int16
	Month time.Month
	Day   uint8
}

// DateOf returns the calendar date of t in loc.
func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{Year: int16(y), Month: m, Day: uint8(d)}
}

// ParseDate accepts "YYYY-MM-DD" or GTFS "YYYYMMDD".
func ParseDate(s string) (Date, error) {
	layout := "2006-01-02"
	if len(s) == 8 {
		layout = "20060102"
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return DateOf(t, time.UTC), nil
}

func (d Date) IsZero() bool { return d == Date{} }

// String formats as "YYYY-MM-DD".
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// GTFS formats as "YYYYMMDD", the form used by GTFS calendars and start_date.
func (d Date) GTFS() string {
	return fmt.Sprintf("%04d%02d%02d", d.Year, int(d.Month), d.Day)
}

// AddDays returns the date n days later (or earlier for negative n).
func (d Date) AddDays(n int) Date {
	t := time.Date(int(d.Year), d.Month, int(d.Day)+n, 12, 0, 0, 0, time.UTC)
	return DateOf(t, time.UTC)
}

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday {
	return time.Date(int(d.Year), d.Month, int(d.Day), 12, 0, 0, 0, time.UTC).Weekday()
}

// Midnight is the start of the service day in loc. GTFS times are offsets from
// this instant and may exceed 24h for trips running past midnight.
func (d Date) Midnight(loc *time.Location) time.Time {
	return time.Date(int(d.Year), d.Month, int(d.Day), 0, 0, 0, 0, loc)
}

// Position is where a call sits relative to the train.
type Position uint8

const (
	Upcoming Position = iota
	Current           // the stop the train is at, or heading to
	Passed
)

func (p Position) String() string {
	switch p {
	case Passed:
		return "passed"
	case Current:
		return "current"
	default:
		return "upcoming"
	}
}

// TimeSource is how we know a call's time.
type TimeSource uint8

const (
	// Scheduled: static time only, no realtime evidence either way.
	Scheduled TimeSource = iota
	// Observed: passed, with an actual time from realtime.
	Observed
	// Predicted: not yet passed, with a time from a live stop time update.
	Predicted
	// Projected: not yet passed, static time plus a forward-propagated delay.
	Projected
)

func (s TimeSource) String() string {
	switch s {
	case Observed:
		return "observed"
	case Predicted:
		return "predicted"
	case Projected:
		return "projected"
	default:
		return "scheduled"
	}
}

// Punctuality classifies a leg or journey. The zero value is NoRealtime: a
// train we have no realtime data for is never reported as on time.
type Punctuality uint8

const (
	NoRealtime Punctuality = iota
	OnTime
	Early
	Late
	VeryLate
	Skipping  // the train will not stop at the boarding station
	Cancelled // the trip will not run
)

func (p Punctuality) String() string {
	switch p {
	case OnTime:
		return "onTime"
	case Early:
		return "early"
	case Late:
		return "late"
	case VeryLate:
		return "veryLate"
	case Skipping:
		return "skipping"
	case Cancelled:
		return "cancelled"
	default:
		return "scheduled"
	}
}

// severity orders punctualities for "worst across legs". A leg with no
// realtime outranks an on-time one, because it is less certain.
func (p Punctuality) severity() int {
	switch p {
	case OnTime:
		return 0
	case NoRealtime:
		return 1
	case Early:
		return 2
	case Late:
		return 3
	case VeryLate:
		return 4
	case Skipping:
		return 5
	case Cancelled:
		return 6
	}
	return 1
}

// Classify maps a delay in seconds to a punctuality band (SPEC.md §3.5).
func Classify(delaySeconds int32) Punctuality {
	switch {
	case delaySeconds < -60:
		return Early
	case delaySeconds < 60:
		return OnTime
	case delaySeconds < 300:
		return Late
	default:
		return VeryLate
	}
}

// Station is a parent station.
type Station struct {
	ID      StationID
	Name    string // "Central station"
	Lat     float64
	Lon     float64
	Aliases []string // curated alternative names that resolve to this station
}

// Point is a WGS84 coordinate.
type Point struct{ Lat, Lon float64 }

// Call is one stop of a leg.
type Call struct {
	Station   StationID // empty when the stop ID is unknown to the static feed
	Stop      StopID    // the platform actually used
	Name      string    // station name, or "Unknown stop <id>"
	Platform  string    // platform code; empty when unknown
	Sequence  uint16
	Scheduled time.Time  // departure (arrival at the final stop)
	Expected  time.Time  // == Scheduled when there is no realtime
	Actual    *time.Time // set once passed and observed
	Position  Position
	Source    TimeSource
	Skipped   bool // the train will not stop here
}

// Delay is Expected minus Scheduled.
func (c Call) Delay() time.Duration { return c.Expected.Sub(c.Scheduled) }

// VehicleFix is the last reported position of the train running a leg.
type VehicleFix struct {
	Lat, Lon   float64
	AtSequence uint16
	Status     string // "STOPPED_AT" | "IN_TRANSIT_TO" | "INCOMING_AT"
	ObservedAt time.Time
	Stale      bool
}

// Alert is a service alert relevant to a leg.
type Alert struct {
	ID          string
	Header      string
	Description string
	Effect      string     // REDUCED_SERVICE | MODIFIED_SERVICE | NO_SERVICE | ...
	Cause       string     // MAINTENANCE | STRIKE | CONSTRUCTION | ...
	ActiveUntil *time.Time // end of the active window containing now; nil = open-ended
}

// Leg is one train ride on one service date.
type Leg struct {
	TripID       TripID
	ServiceDate  Date
	RunNumber    string // vehicle.label / trip ID suffix, e.g. "1776"
	RouteName    string // route_long_name
	RouteColor   string // "#D53E45"
	Headsign     string
	Calls        []Call // every stop, in sequence order
	Board        int    // index into Calls
	Alight       int    // index into Calls
	Punctuality  Punctuality
	DelaySeconds int32 // delay at the boarding call
	Vehicle      *VehicleFix
	Alerts       []Alert
	ShapeID      ShapeID // empty for ADDED trips
	Added        bool    // an unscheduled trip with no static counterpart
}

// Depart is the expected departure from the boarding call.
func (l *Leg) Depart() time.Time { return l.Calls[l.Board].Expected }

// Arrive is the expected arrival at the alighting call.
func (l *Leg) Arrive() time.Time { return l.Calls[l.Alight].Expected }

// Change describes the interchange of a one-change journey.
type Change struct {
	Station StationID
	Name    string
	Buffer  time.Duration // expected arrival of leg 1 to expected departure of leg 2
	Tight   bool          // Buffer < 5 min
}

// Journey is a way to get from origin to destination: one direct leg, or two
// legs with one change.
type Journey struct {
	Legs        []Leg
	Change      *Change // nil when direct
	Punctuality Punctuality
}

// Depart is the expected departure of the first leg.
func (j *Journey) Depart() time.Time { return j.Legs[0].Depart() }

// Arrive is the expected arrival of the final leg.
func (j *Journey) Arrive() time.Time { return j.Legs[len(j.Legs)-1].Arrive() }

// Freshness describes the realtime data behind an answer.
type Freshness struct {
	GeneratedAt time.Time
	DataAge     time.Duration // age of the trip update feed; meaningless when Degraded
	Degraded    bool          // realtime unavailable or older than 90 s
}
