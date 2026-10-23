// Package gtfsrt fetches and decodes the Translink SEQ GTFS-realtime rail feeds
// (trip updates, vehicle positions, alerts) into immutable snapshots, and keeps a
// per-service-day table of observed stop events.
package gtfsrt

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// Feeds holds the URLs of the three realtime feeds.
type Feeds struct{ TripUpdates, VehiclePositions, Alerts string }

// DefaultBase is Translink SEQ's GTFS-RT root.
const DefaultBase = "https://gtfsrt.api.translink.com.au/api/realtime/SEQ"

// DefaultFeeds are the public Translink SEQ rail feeds.
var DefaultFeeds = FeedsAt(DefaultBase)

// FeedsAt returns the rail feeds under a GTFS-RT base URL laid out like
// Translink's: <base>/TripUpdates/Rail, <base>/VehiclePositions/Rail and
// <base>/Alerts.
func FeedsAt(base string) Feeds {
	base = strings.TrimRight(base, "/")
	return Feeds{
		TripUpdates:      base + "/TripUpdates/Rail",
		VehiclePositions: base + "/VehiclePositions/Rail",
		Alerts:           base + "/Alerts",
	}
}

// TripRel is the TripDescriptor schedule relationship.
type TripRel uint8

// Trip schedule relationships.
const (
	RelScheduled TripRel = iota
	RelAdded
	RelUnscheduled
	RelCanceled
	RelReplacement
	RelDuplicated
	RelDeleted
)

// Event is a stop-time event (arrival or departure).
type Event struct {
	Time     int64 // unix seconds, 0 = absent
	Delay    int32 // seconds
	HasTime  bool
	HasDelay bool
}

// StopTimeUpdate is one stop-level prediction within a TripUpdate.
type StopTimeUpdate struct {
	Seq            uint32
	HasSeq         bool
	StopID         string
	AssignedStopID string // stop_time_properties.assigned_stop_id
	Arrival        Event
	Departure      Event
	HasArrival     bool
	HasDeparture   bool
	Skipped        bool // schedule_relationship SKIPPED
	NoData         bool // schedule_relationship NO_DATA
}

// TripUpdate is a decoded realtime trip update.
type TripUpdate struct {
	TripID, RouteID, StartDate, StartTime string // StartDate YYYYMMDD
	Rel                                   TripRel
	VehicleID, VehicleLabel               string
	Updates                               []StopTimeUpdate // sorted by Seq when every update HasSeq (feed order otherwise)
}

// VehicleStatus is the vehicle's status relative to its current stop.
type VehicleStatus uint8

// Vehicle stop statuses.
const (
	StatusInTransitTo VehicleStatus = iota
	StatusStoppedAt
	StatusIncomingAt
)

// String returns the proto enum name.
func (s VehicleStatus) String() string {
	switch s {
	case StatusStoppedAt:
		return "STOPPED_AT"
	case StatusIncomingAt:
		return "INCOMING_AT"
	default:
		return "IN_TRANSIT_TO"
	}
}

// Vehicle is a decoded vehicle position.
type Vehicle struct {
	TripID, RouteID, ID, Label string
	Lat, Lon                   float64
	Seq                        uint32 // current_stop_sequence
	StopID                     string
	Status                     VehicleStatus
	Timestamp                  time.Time // zero if absent
	Stale                      bool      // feed header time - Timestamp > 120 s, or Timestamp absent
}

// Period is an alert active period; a zero bound is unbounded.
type Period struct{ Start, End time.Time }

// Alert is a decoded service alert.
type Alert struct {
	ID, Header, Description, Effect, Cause string // Effect/Cause as proto enum names
	Periods                                []Period
	Routes, Trips, Stops                   []string // informed_entity route_id / trip.trip_id / stop_id
}

// ActiveAt reports whether the alert is active at t. An alert without periods is
// always active. until is the End of the containing period (zero if open-ended;
// if several contain t, the latest End, with zero winning).
func (a *Alert) ActiveAt(t time.Time) (active bool, until time.Time) {
	if len(a.Periods) == 0 {
		return true, time.Time{}
	}
	open := false
	for _, p := range a.Periods {
		if (!p.Start.IsZero() && t.Before(p.Start)) || (!p.End.IsZero() && !t.Before(p.End)) {
			continue
		}
		active = true
		if p.End.IsZero() {
			open = true
		} else if p.End.After(until) {
			until = p.End
		}
	}
	if open {
		until = time.Time{}
	}
	return active, until
}

// Staleness thresholds (SPEC §3.4).
const (
	vehicleStaleAfter = 120 * time.Second
	tripDropAfter     = 60 * time.Minute
)

// tripDescriptorDeleted is DELETED, absent from the pinned bindings' enum.
const tripDescriptorDeleted gtfs.TripDescriptor_ScheduleRelationship = 7

func unmarshalFeed(b []byte) (*gtfs.FeedMessage, time.Time, error) {
	var m gtfs.FeedMessage
	if err := proto.Unmarshal(b, &m); err != nil {
		return nil, time.Time{}, fmt.Errorf("unmarshal feed message: %w", err)
	}
	return &m, unixOrZero(m.GetHeader().GetTimestamp()), nil
}

func unixOrZero(s uint64) time.Time {
	if s == 0 {
		return time.Time{}
	}
	return time.Unix(int64(s), 0)
}

func tripRel(r gtfs.TripDescriptor_ScheduleRelationship) TripRel {
	switch r {
	case gtfs.TripDescriptor_ADDED:
		return RelAdded
	case gtfs.TripDescriptor_UNSCHEDULED:
		return RelUnscheduled
	case gtfs.TripDescriptor_CANCELED:
		return RelCanceled
	case gtfs.TripDescriptor_REPLACEMENT:
		return RelReplacement
	case gtfs.TripDescriptor_DUPLICATED:
		return RelDuplicated
	case tripDescriptorDeleted:
		return RelDeleted
	default:
		return RelScheduled
	}
}

func event(e *gtfs.TripUpdate_StopTimeEvent) Event {
	return Event{
		Time:     e.GetTime(),
		Delay:    e.GetDelay(),
		HasTime:  e.Time != nil,
		HasDelay: e.Delay != nil,
	}
}

// DecodeTripUpdates decodes a TripUpdates feed keyed by trip_id. A trip update
// whose every stop time is more than 60 minutes before now is dropped; trip
// updates carrying no stop times are kept.
func DecodeTripUpdates(b []byte, now time.Time) (trips map[string]*TripUpdate, header time.Time, err error) {
	m, header, err := unmarshalFeed(b)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("decode trip updates: %w", err)
	}
	cutoff := now.Add(-tripDropAfter).Unix()
	trips = make(map[string]*TripUpdate, len(m.GetEntity()))
	for _, ent := range m.GetEntity() {
		pu := ent.GetTripUpdate()
		if pu == nil || ent.GetIsDeleted() {
			continue
		}
		td := pu.GetTrip()
		id := td.GetTripId()
		if id == "" {
			continue
		}
		tu := &TripUpdate{
			TripID:       id,
			RouteID:      td.GetRouteId(),
			StartDate:    td.GetStartDate(),
			StartTime:    td.GetStartTime(),
			Rel:          tripRel(td.GetScheduleRelationship()),
			VehicleID:    pu.GetVehicle().GetId(),
			VehicleLabel: pu.GetVehicle().GetLabel(),
			Updates:      make([]StopTimeUpdate, 0, len(pu.GetStopTimeUpdate())),
		}
		hasTimes, recent, allSeq := false, false, true
		for _, ps := range pu.GetStopTimeUpdate() {
			u := StopTimeUpdate{
				Seq:            ps.GetStopSequence(),
				HasSeq:         ps.StopSequence != nil,
				StopID:         ps.GetStopId(),
				AssignedStopID: ps.GetStopTimeProperties().GetAssignedStopId(),
				HasArrival:     ps.Arrival != nil,
				HasDeparture:   ps.Departure != nil,
			}
			if u.HasArrival {
				u.Arrival = event(ps.Arrival)
			}
			if u.HasDeparture {
				u.Departure = event(ps.Departure)
			}
			switch ps.GetScheduleRelationship() {
			case gtfs.TripUpdate_StopTimeUpdate_SKIPPED:
				u.Skipped = true
			case gtfs.TripUpdate_StopTimeUpdate_NO_DATA:
				u.NoData = true
			}
			for _, e := range [2]Event{u.Arrival, u.Departure} {
				if e.HasTime {
					hasTimes = true
					if e.Time >= cutoff {
						recent = true
					}
				}
			}
			allSeq = allSeq && u.HasSeq
			tu.Updates = append(tu.Updates, u)
		}
		if hasTimes && !recent {
			continue
		}
		if allSeq {
			slices.SortStableFunc(tu.Updates, func(a, b StopTimeUpdate) int { return cmp.Compare(a.Seq, b.Seq) })
		}
		trips[id] = tu
	}
	return trips, header, nil
}

// DecodeVehicles decodes a VehiclePositions feed keyed by trip_id. A vehicle is
// stale when its timestamp is absent or more than 120 s behind the feed header.
func DecodeVehicles(b []byte) (vehicles map[string]*Vehicle, header time.Time, err error) {
	m, header, err := unmarshalFeed(b)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("decode vehicle positions: %w", err)
	}
	vehicles = make(map[string]*Vehicle, len(m.GetEntity()))
	for _, ent := range m.GetEntity() {
		pv := ent.GetVehicle()
		if pv == nil || ent.GetIsDeleted() {
			continue
		}
		id := pv.GetTrip().GetTripId()
		if id == "" {
			continue
		}
		v := &Vehicle{
			TripID:    id,
			RouteID:   pv.GetTrip().GetRouteId(),
			ID:        pv.GetVehicle().GetId(),
			Label:     pv.GetVehicle().GetLabel(),
			Lat:       float64(pv.GetPosition().GetLatitude()),
			Lon:       float64(pv.GetPosition().GetLongitude()),
			Seq:       pv.GetCurrentStopSequence(),
			StopID:    pv.GetStopId(),
			Timestamp: unixOrZero(pv.GetTimestamp()),
		}
		switch pv.GetCurrentStatus() {
		case gtfs.VehiclePosition_STOPPED_AT:
			v.Status = StatusStoppedAt
		case gtfs.VehiclePosition_INCOMING_AT:
			v.Status = StatusIncomingAt
		default:
			v.Status = StatusInTransitTo
		}
		v.Stale = v.Timestamp.IsZero() || header.Sub(v.Timestamp) > vehicleStaleAfter
		vehicles[id] = v
	}
	return vehicles, header, nil
}

// DecodeAlerts decodes an Alerts feed in feed order.
func DecodeAlerts(b []byte) (alerts []*Alert, header time.Time, err error) {
	m, header, err := unmarshalFeed(b)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("decode alerts: %w", err)
	}
	for _, ent := range m.GetEntity() {
		pa := ent.GetAlert()
		if pa == nil || ent.GetIsDeleted() {
			continue
		}
		a := &Alert{
			ID:          ent.GetId(),
			Header:      translation(pa.GetHeaderText()),
			Description: translation(pa.GetDescriptionText()),
			Effect:      pa.GetEffect().String(),
			Cause:       pa.GetCause().String(),
		}
		for _, p := range pa.GetActivePeriod() {
			a.Periods = append(a.Periods, Period{Start: unixOrZero(p.GetStart()), End: unixOrZero(p.GetEnd())})
		}
		for _, e := range pa.GetInformedEntity() {
			if r := e.GetRouteId(); r != "" && !slices.Contains(a.Routes, r) {
				a.Routes = append(a.Routes, r)
			}
			if t := e.GetTrip().GetTripId(); t != "" && !slices.Contains(a.Trips, t) {
				a.Trips = append(a.Trips, t)
			}
			if s := e.GetStopId(); s != "" && !slices.Contains(a.Stops, s) {
				a.Stops = append(a.Stops, s)
			}
		}
		alerts = append(alerts, a)
	}
	return alerts, header, nil
}

// translation returns the English text if present, else the first translation.
func translation(ts *gtfs.TranslatedString) string {
	tr := ts.GetTranslation()
	for _, t := range tr {
		if l := strings.ToLower(t.GetLanguage()); l == "en" || strings.HasPrefix(l, "en-") {
			return t.GetText()
		}
	}
	if len(tr) > 0 {
		return tr[0].GetText()
	}
	return ""
}
