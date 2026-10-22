// Package gtfs parses the Translink SEQ static GTFS zip into a compact,
// rail-only index and caches that index on disk as gob, keyed by ETag.
package gtfs

import (
	"maps"
	"slices"
	"time"

	"trains/gtfsrail/internal/geo"
)

// Route is a rail route (route_type 2).
type Route struct {
	ID, ShortName, LongName string
	// Color and TextColor are hex without '#', exactly as in the feed.
	Color, TextColor string
}

// Stop is a platform: a stop whose parent_station is a rail station.
type Stop struct {
	ID, Code, Name string
	Lat, Lon       float64
	// Parent is the parent station ID; it is always a key of Index.Stations.
	Parent string
	// Platform is the feed's platform_code, e.g. "3".
	Platform string
}

// Station is a rail parent station (location_type 1).
type Station struct {
	ID, Name string
	Lat, Lon float64
	// Platforms holds the IDs of this station's Stops, sorted.
	Platforms []string
}

// StopTime is one scheduled call of a trip.
type StopTime struct {
	StopID                  string
	Seq                     uint16
	Arrival, Departure      Secs
	PickupType, DropOffType uint8
}

// Trip is a rail trip. Its calls are Index.StopTimes[First:First+Count],
// sorted by Seq.
type Trip struct {
	ID, RouteID, ServiceID, ShapeID, Headsign string
	Direction                                 uint8
	First, Count                              int32
}

// Calendar is one calendar.txt row.
type Calendar struct {
	// Days is indexed by time.Weekday (Sunday = 0).
	Days [7]bool
	// Start and End are YYYYMMDD, inclusive.
	Start, End string
}

// Index is the rail subset of one static GTFS publication. It is immutable
// once built and safe for concurrent reads.
type Index struct {
	// ETag is the HTTP ETag of the zip the index was built from ("" if unknown).
	ETag string
	// FeedStart and FeedEnd are feed_info's YYYYMMDD validity range.
	FeedStart, FeedEnd string
	// Routes holds rail routes only (route_type 2).
	Routes map[string]*Route
	// Stops holds every platform whose parent is a rail station, including
	// platforms no static trip uses. Stops served by non-rail trips (bus
	// stops, light-rail platforms that share a parent) are excluded.
	Stops map[string]*Stop
	// Stations holds rail parent stations: parents of platforms that rail
	// trips call at.
	Stations map[string]*Station
	// Trips holds rail trips only.
	Trips map[string]*Trip
	// StopTimes is the single backing array for every trip's calls.
	StopTimes []StopTime
	// Shapes holds rail shapes, simplified to at most MaxShapePoints points.
	Shapes map[string][]geo.Point
	// Calendars holds calendar.txt rows for rail services.
	Calendars map[string]Calendar
	// CalendarDates maps service ID -> YYYYMMDD -> exception type
	// (1 added, 2 removed), for rail services.
	CalendarDates map[string]map[string]uint8
	// Calling maps a station ID to the IDs of trips calling there; sorted, unique.
	Calling map[string][]string
}

// MaxShapePoints is the point budget for each simplified shape.
const MaxShapePoints = 120

// TripStopTimes returns t's calls in stop_sequence order. The slice aliases
// the index and must not be modified. It returns nil for a trip whose range
// does not fit this index.
func (ix *Index) TripStopTimes(t *Trip) []StopTime {
	if t == nil || t.First < 0 || t.Count < 0 || int(t.First)+int(t.Count) > len(ix.StopTimes) {
		return nil
	}
	return ix.StopTimes[t.First : t.First+t.Count : t.First+t.Count]
}

// StationOf returns the station for a platform ID or a station ID.
func (ix *Index) StationOf(stopID string) (*Station, bool) {
	if st, ok := ix.Stations[stopID]; ok {
		return st, true
	}
	if s, ok := ix.Stops[stopID]; ok {
		st, ok := ix.Stations[s.Parent]
		return st, ok
	}
	return nil, false
}

// ServiceActive applies calendar weekday+date range, then calendar_dates
// overrides (1 adds, 2 removes). date is YYYYMMDD; a malformed date is never
// active.
func (ix *Index) ServiceActive(serviceID, date string) bool {
	d, err := time.Parse("20060102", date)
	if err != nil {
		return false
	}
	switch ix.CalendarDates[serviceID][date] {
	case 1:
		return true
	case 2:
		return false
	}
	c, ok := ix.Calendars[serviceID]
	return ok && date >= c.Start && date <= c.End && c.Days[d.Weekday()]
}

// buildCalling derives Calling from Trips and StopTimes. Trips are visited in
// ID order, so each list comes out sorted; a trip calling twice at a station
// is recorded once because its earlier call left it last in that list.
func (ix *Index) buildCalling() {
	ix.Calling = make(map[string][]string, len(ix.Stations))
	for _, id := range slices.Sorted(maps.Keys(ix.Trips)) {
		tr := ix.Trips[id]
		for _, st := range ix.TripStopTimes(tr) {
			s, ok := ix.Stops[st.StopID]
			if !ok {
				continue
			}
			if c := ix.Calling[s.Parent]; len(c) == 0 || c[len(c)-1] != tr.ID {
				ix.Calling[s.Parent] = append(c, tr.ID)
			}
		}
	}
}
