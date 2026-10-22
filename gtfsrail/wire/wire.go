// Package wire is the JSON contract shared by `trains serve` and
// `trains --json`. web/src/api/types.ts mirrors it field for field.
//
// Conventions: camelCase keys, timestamps RFC3339 in Australia/Brisbane,
// durations as integer seconds, and absent values as null rather than omitted.
package wire

import (
	"time"

	"trains/gtfsrail"
)

type Station struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	Aliases []string `json:"aliases"`
}

type Call struct {
	StationID string  `json:"stationId"`
	Name      string  `json:"name"`
	Platform  *string `json:"platform"`
	Scheduled string  `json:"scheduled"`
	Expected  string  `json:"expected"`
	Actual    *string `json:"actual"`
	Position  string  `json:"position"`
	Source    string  `json:"source"`
	Skipped   bool    `json:"skipped"`
}

type VehicleFix struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	AtSequence int     `json:"atSequence"`
	// AtIndex is the index into Leg.Calls of the call at AtSequence, or null
	// when that sequence is not part of the leg.
	AtIndex    *int   `json:"atIndex"`
	Status     string `json:"status"`
	ObservedAt string `json:"observedAt"`
	Stale      bool   `json:"stale"`
}

type Alert struct {
	ID          string  `json:"id"`
	Header      string  `json:"header"`
	Description string  `json:"description"`
	Effect      string  `json:"effect"`
	Cause       string  `json:"cause"`
	ActiveUntil *string `json:"activeUntil"`
}

type Leg struct {
	TripID       string      `json:"tripId"`
	ServiceDate  string      `json:"serviceDate"`
	RunNumber    *string     `json:"runNumber"`
	RouteName    string      `json:"routeName"`
	RouteColor   string      `json:"routeColor"`
	Headsign     string      `json:"headsign"`
	Calls        []Call      `json:"calls"`
	Board        int         `json:"board"`
	Alight       int         `json:"alight"`
	Punctuality  string      `json:"punctuality"`
	DelaySeconds int32       `json:"delaySeconds"`
	Vehicle      *VehicleFix `json:"vehicle"`
	Alerts       []Alert     `json:"alerts"`
	ShapeID      *string     `json:"shapeId"`
}

type Change struct {
	StationID     string `json:"stationId"`
	Name          string `json:"name"`
	BufferSeconds int64  `json:"bufferSeconds"`
	Tight         bool   `json:"tight"`
}

type Journey struct {
	Legs        []Leg   `json:"legs"`
	Change      *Change `json:"change"`
	Depart      string  `json:"depart"`
	Arrive      string  `json:"arrive"`
	Punctuality string  `json:"punctuality"`
}

type JourneysResponse struct {
	Origin         Station   `json:"origin"`
	Destination    Station   `json:"destination"`
	Journeys       []Journey `json:"journeys"`
	GeneratedAt    string    `json:"generatedAt"`
	DataAgeSeconds int64     `json:"dataAgeSeconds"`
	Degraded       bool      `json:"degraded"`
}

type LegResponse struct {
	Leg            Leg    `json:"leg"`
	GeneratedAt    string `json:"generatedAt"`
	DataAgeSeconds int64  `json:"dataAgeSeconds"`
	Degraded       bool   `json:"degraded"`
}

type ShapeResponse struct {
	ShapeID string       `json:"shapeId"`
	Points  [][2]float64 `json:"points"` // [lat, lon]
}

type StationsResponse struct {
	Stations []Station `json:"stations"`
}

// Error codes for ErrorResponse.
const (
	ErrAmbiguous      = "ambiguous"
	ErrUnknownStation = "unknown_station"
	ErrNotFound       = "not_found"
	ErrBadRequest     = "bad_request"
)

type ErrorResponse struct {
	Error      string    `json:"error"`
	Message    string    `json:"message"`
	Candidates []Station `json:"candidates,omitempty"`
}

type FeedHealth struct {
	LastSuccess *string `json:"lastSuccess"`
	AgeSeconds  *int64  `json:"ageSeconds"`
	LastError   *string `json:"lastError"`
	Fetches     int64   `json:"fetches"`
}

type HealthResponse struct {
	TripUpdates      FeedHealth `json:"tripUpdates"`
	VehiclePositions FeedHealth `json:"vehiclePositions"`
	Alerts           FeedHealth `json:"alerts"`
	Degraded         bool       `json:"degraded"`
}

// Time formats t as RFC3339 in Australia/Brisbane.
func Time(t time.Time) string { return t.In(gtfsrail.Brisbane).Format(time.RFC3339) }

func timePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := Time(*t)
	return &s
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func FromStation(s gtfsrail.Station) Station {
	aliases := s.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return Station{ID: string(s.ID), Name: s.Name, Lat: s.Lat, Lon: s.Lon, Aliases: aliases}
}

func FromStations(ss []gtfsrail.Station) []Station {
	out := make([]Station, len(ss))
	for i, s := range ss {
		out[i] = FromStation(s)
	}
	return out
}

func FromCall(c gtfsrail.Call) Call {
	return Call{
		StationID: string(c.Station),
		Name:      c.Name,
		Platform:  strPtr(c.Platform),
		Scheduled: Time(c.Scheduled),
		Expected:  Time(c.Expected),
		Actual:    timePtr(c.Actual),
		Position:  c.Position.String(),
		Source:    c.Source.String(),
		Skipped:   c.Skipped,
	}
}

func FromLeg(l *gtfsrail.Leg) Leg {
	calls := make([]Call, len(l.Calls))
	for i, c := range l.Calls {
		calls[i] = FromCall(c)
	}
	alerts := make([]Alert, len(l.Alerts))
	for i, a := range l.Alerts {
		alerts[i] = Alert{
			ID: a.ID, Header: a.Header, Description: a.Description,
			Effect: a.Effect, Cause: a.Cause, ActiveUntil: timePtr(a.ActiveUntil),
		}
	}
	var vehicle *VehicleFix
	if v := l.Vehicle; v != nil {
		vehicle = &VehicleFix{
			Lat: v.Lat, Lon: v.Lon, AtSequence: int(v.AtSequence),
			Status: v.Status, ObservedAt: Time(v.ObservedAt), Stale: v.Stale,
		}
		for i, c := range l.Calls {
			if c.Sequence == v.AtSequence {
				vehicle.AtIndex = &i
				break
			}
		}
	}
	return Leg{
		TripID:       string(l.TripID),
		ServiceDate:  l.ServiceDate.String(),
		RunNumber:    strPtr(l.RunNumber),
		RouteName:    l.RouteName,
		RouteColor:   l.RouteColor,
		Headsign:     l.Headsign,
		Calls:        calls,
		Board:        l.Board,
		Alight:       l.Alight,
		Punctuality:  l.Punctuality.String(),
		DelaySeconds: l.DelaySeconds,
		Vehicle:      vehicle,
		Alerts:       alerts,
		ShapeID:      strPtr(string(l.ShapeID)),
	}
}

func FromJourney(j *gtfsrail.Journey) Journey {
	legs := make([]Leg, len(j.Legs))
	for i := range j.Legs {
		legs[i] = FromLeg(&j.Legs[i])
	}
	var change *Change
	if c := j.Change; c != nil {
		change = &Change{
			StationID:     string(c.Station),
			Name:          c.Name,
			BufferSeconds: int64(c.Buffer / time.Second),
			Tight:         c.Tight,
		}
	}
	return Journey{
		Legs:        legs,
		Change:      change,
		Depart:      Time(j.Depart()),
		Arrive:      Time(j.Arrive()),
		Punctuality: j.Punctuality.String(),
	}
}

func FromJourneys(origin, dest gtfsrail.Station, js []gtfsrail.Journey, f gtfsrail.Freshness) JourneysResponse {
	out := make([]Journey, len(js))
	for i := range js {
		out[i] = FromJourney(&js[i])
	}
	return JourneysResponse{
		Origin:         FromStation(origin),
		Destination:    FromStation(dest),
		Journeys:       out,
		GeneratedAt:    Time(f.GeneratedAt),
		DataAgeSeconds: int64(f.DataAge / time.Second),
		Degraded:       f.Degraded,
	}
}

func FromLegResponse(l *gtfsrail.Leg, f gtfsrail.Freshness) LegResponse {
	return LegResponse{
		Leg:            FromLeg(l),
		GeneratedAt:    Time(f.GeneratedAt),
		DataAgeSeconds: int64(f.DataAge / time.Second),
		Degraded:       f.Degraded,
	}
}

func FromShape(id gtfsrail.ShapeID, pts []gtfsrail.Point) ShapeResponse {
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{p.Lat, p.Lon}
	}
	return ShapeResponse{ShapeID: string(id), Points: out}
}
