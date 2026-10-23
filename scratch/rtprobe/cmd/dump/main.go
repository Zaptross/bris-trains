package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

type STU struct {
	Seq        uint32 `json:"seq"`
	StopID     string `json:"stop_id"`
	Assigned   string `json:"assigned_stop_id,omitempty"`
	ArrTime    int64  `json:"arr_time,omitempty"`
	ArrDelay   int32  `json:"arr_delay,omitempty"`
	DepTime    int64  `json:"dep_time,omitempty"`
	DepDelay   int32  `json:"dep_delay,omitempty"`
	SchedRel   string `json:"stu_rel"`
	HasArrival bool   `json:"has_arr"`
}

type TU struct {
	EntityID string  `json:"entity_id"`
	TripID   string  `json:"trip_id"`
	RouteID  string  `json:"route_id"`
	StartTS  string  `json:"start_time"`
	StartDT  string  `json:"start_date"`
	SchedRel string  `json:"trip_rel"`
	DirID    *uint32 `json:"direction_id,omitempty"`
	VehID    string  `json:"vehicle_id,omitempty"`
	STUs     []STU   `json:"stus"`
}

type VP struct {
	EntityID string  `json:"entity_id"`
	TripID   string  `json:"trip_id"`
	RouteID  string  `json:"route_id"`
	VehID    string  `json:"vehicle_id"`
	Label    string  `json:"label"`
	Lat      float32 `json:"lat"`
	Lon      float32 `json:"lon"`
	Seq      uint32  `json:"cur_seq"`
	StopID   string  `json:"stop_id"`
	Status   string  `json:"status"`
	TS       uint64  `json:"ts"`
}

type Alert struct {
	EntityID string      `json:"entity_id"`
	Cause    string      `json:"cause"`
	Effect   string      `json:"effect"`
	Routes   []string    `json:"routes"`
	Stops    []string    `json:"stops"`
	Trips    []string    `json:"trips"`
	Header   string      `json:"header"`
	Desc     string      `json:"desc"`
	Periods  [][2]uint64 `json:"periods"`
}

func fetch(url string) *gtfs.FeedMessage {
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m gtfs.FeedMessage
	if err := proto.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return &m
}

func main() {
	base := "https://gtfsrt.api.translink.com.au/api/realtime/SEQ/"
	tuf := fetch(base + "TripUpdates/Rail")
	vpf := fetch(base + "VehiclePositions/Rail")
	alf := fetch(base + "Alerts")

	out := map[string]any{"header_ts": tuf.Header.GetTimestamp()}

	var tus []TU
	for _, e := range tuf.Entity {
		t := e.TripUpdate
		if t == nil {
			continue
		}
		rec := TU{EntityID: e.GetId(), TripID: t.Trip.GetTripId(), RouteID: t.Trip.GetRouteId(),
			StartTS: t.Trip.GetStartTime(), StartDT: t.Trip.GetStartDate(),
			SchedRel: t.Trip.GetScheduleRelationship().String(), VehID: t.Vehicle.GetId()}
		if t.Trip.DirectionId != nil {
			d := t.Trip.GetDirectionId()
			rec.DirID = &d
		}
		for _, s := range t.StopTimeUpdate {
			u := STU{Seq: s.GetStopSequence(), StopID: s.GetStopId(), SchedRel: s.GetScheduleRelationship().String()}
			if s.StopTimeProperties != nil {
				u.Assigned = s.StopTimeProperties.GetAssignedStopId()
			}
			if s.Arrival != nil {
				u.HasArrival = true
				u.ArrTime, u.ArrDelay = s.Arrival.GetTime(), s.Arrival.GetDelay()
			}
			if s.Departure != nil {
				u.DepTime, u.DepDelay = s.Departure.GetTime(), s.Departure.GetDelay()
			}
			rec.STUs = append(rec.STUs, u)
		}
		tus = append(tus, rec)
	}
	out["trip_updates"] = tus

	var vps []VP
	for _, e := range vpf.Entity {
		v := e.Vehicle
		if v == nil {
			continue
		}
		vps = append(vps, VP{EntityID: e.GetId(), TripID: v.Trip.GetTripId(), RouteID: v.Trip.GetRouteId(),
			VehID: v.Vehicle.GetId(), Label: v.Vehicle.GetLabel(),
			Lat: v.Position.GetLatitude(), Lon: v.Position.GetLongitude(),
			Seq: v.GetCurrentStopSequence(), StopID: v.GetStopId(),
			Status: v.GetCurrentStatus().String(), TS: v.GetTimestamp()})
	}
	out["vehicles"] = vps

	var als []Alert
	for _, e := range alf.Entity {
		a := e.Alert
		if a == nil {
			continue
		}
		rec := Alert{EntityID: e.GetId(), Cause: a.GetCause().String(), Effect: a.GetEffect().String()}
		for _, inf := range a.InformedEntity {
			if inf.RouteId != nil {
				rec.Routes = append(rec.Routes, inf.GetRouteId())
			}
			if inf.StopId != nil {
				rec.Stops = append(rec.Stops, inf.GetStopId())
			}
			if inf.Trip != nil {
				rec.Trips = append(rec.Trips, inf.Trip.GetTripId())
			}
		}
		for _, p := range a.ActivePeriod {
			rec.Periods = append(rec.Periods, [2]uint64{p.GetStart(), p.GetEnd()})
		}
		if a.HeaderText != nil && len(a.HeaderText.Translation) > 0 {
			rec.Header = a.HeaderText.Translation[0].GetText()
		}
		if a.DescriptionText != nil && len(a.DescriptionText.Translation) > 0 {
			rec.Desc = a.DescriptionText.Translation[0].GetText()
		}
		als = append(als, rec)
	}
	out["alerts"] = als

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	_ = enc.Encode(out)
}
