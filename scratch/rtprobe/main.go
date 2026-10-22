package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func fetch(url string) *gtfs.FeedMessage {
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  %s -> %d bytes\n", url, len(b))
	var m gtfs.FeedMessage
	if err := proto.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return &m
}

func main() {
	base := "https://gtfsrt.api.translink.com.au/api/realtime/SEQ/"
	fmt.Println("== fetch ==")
	tu := fetch(base + "TripUpdates/Rail")
	vp := fetch(base + "VehiclePositions/Rail")
	al := fetch(base + "Alerts")

	fmt.Printf("\n== header ==\nversion=%s incrementality=%v ts=%d (%s)\n",
		tu.Header.GetGtfsRealtimeVersion(), tu.Header.GetIncrementality(),
		tu.Header.GetTimestamp(), time.Unix(int64(tu.Header.GetTimestamp()), 0).Format(time.RFC3339))

	fmt.Printf("\n== TripUpdates/Rail: %d entities ==\n", len(tu.Entity))
	stuCount := map[int]int{}
	var schedRel = map[string]int{}
	var stuSchedRel = map[string]int{}
	hasVehicle, hasDelay, hasStartDate, hasArr, hasDep, hasUncertainty, hasStopSeq, hasStopID := 0, 0, 0, 0, 0, 0, 0, 0
	tripIDs := map[string]bool{}
	for _, e := range tu.Entity {
		t := e.TripUpdate
		if t == nil {
			continue
		}
		tripIDs[t.Trip.GetTripId()] = true
		schedRel[t.Trip.GetScheduleRelationship().String()]++
		if t.Vehicle != nil {
			hasVehicle++
		}
		if t.Delay != nil {
			hasDelay++
		}
		if t.Trip.StartDate != nil {
			hasStartDate++
		}
		n := len(t.StopTimeUpdate)
		if n < 5 {
			stuCount[n]++
		} else {
			stuCount[5]++
		}
		for _, s := range t.StopTimeUpdate {
			stuSchedRel[s.GetScheduleRelationship().String()]++
			if s.Arrival != nil {
				hasArr++
				if s.Arrival.Uncertainty != nil {
					hasUncertainty++
				}
			}
			if s.Departure != nil {
				hasDep++
			}
			if s.StopSequence != nil {
				hasStopSeq++
			}
			if s.StopId != nil {
				hasStopID++
			}
		}
	}
	fmt.Printf("trip.schedule_relationship: %v\n", schedRel)
	fmt.Printf("entities with vehicle=%d delay=%d start_date=%d\n", hasVehicle, hasDelay, hasStartDate)
	fmt.Printf("stop_time_update count buckets (5=>=5): %v\n", stuCount)
	fmt.Printf("stu.schedule_relationship: %v\n", stuSchedRel)
	fmt.Printf("stu fields: arrival=%d departure=%d stop_seq=%d stop_id=%d uncertainty=%d\n",
		hasArr, hasDep, hasStopSeq, hasStopID, hasUncertainty)

	// dump one full trip update
	fmt.Println("\n== sample TripUpdate (most stop_time_updates) ==")
	best := 0
	for i, e := range tu.Entity {
		if e.TripUpdate != nil && len(e.TripUpdate.StopTimeUpdate) > len(tu.Entity[best].TripUpdate.GetStopTimeUpdate()) {
			best = i
		}
	}
	e := tu.Entity[best]
	fmt.Printf("id=%s\n%s\n", e.GetId(), prototext(e.TripUpdate))

	fmt.Printf("\n== VehiclePositions/Rail: %d entities ==\n", len(vp.Entity))
	vhasTrip, vhasPos, vhasBearing, vhasSpeed, vhasStop, vhasStatus, vhasOcc, vhasLabel, vhasCongestion := 0, 0, 0, 0, 0, 0, 0, 0, 0
	matched := 0
	statusC := map[string]int{}
	for _, e := range vp.Entity {
		v := e.Vehicle
		if v == nil {
			continue
		}
		if v.Trip != nil {
			vhasTrip++
			if tripIDs[v.Trip.GetTripId()] {
				matched++
			}
		}
		if v.Position != nil {
			vhasPos++
			if v.Position.Bearing != nil {
				vhasBearing++
			}
			if v.Position.Speed != nil {
				vhasSpeed++
			}
		}
		if v.StopId != nil {
			vhasStop++
		}
		if v.CurrentStatus != nil {
			vhasStatus++
			statusC[v.GetCurrentStatus().String()]++
		}
		if v.OccupancyStatus != nil {
			vhasOcc++
		}
		if v.Vehicle != nil && v.Vehicle.Label != nil {
			vhasLabel++
		}
		if v.CongestionLevel != nil {
			vhasCongestion++
		}
	}
	fmt.Printf("trip=%d (matched TU trip_id=%d) position=%d bearing=%d speed=%d stop_id=%d status=%d occupancy=%d label=%d congestion=%d\n",
		vhasTrip, matched, vhasPos, vhasBearing, vhasSpeed, vhasStop, vhasStatus, vhasOcc, vhasLabel, vhasCongestion)
	fmt.Printf("current_status: %v\n", statusC)
	if len(vp.Entity) > 0 {
		fmt.Printf("\n== sample VehiclePosition ==\nid=%s\n%s\n", vp.Entity[0].GetId(), prototext(vp.Entity[0].Vehicle))
	}

	fmt.Printf("\n== Alerts: %d entities ==\n", len(al.Entity))
	railAlerts := 0
	for _, e := range al.Entity {
		a := e.Alert
		if a == nil {
			continue
		}
		for _, inf := range a.InformedEntity {
			if inf.RouteType != nil && inf.GetRouteType() == 2 {
				railAlerts++
				break
			}
			if inf.Trip != nil && tripIDs[inf.Trip.GetTripId()] {
				railAlerts++
				break
			}
		}
	}
	fmt.Printf("alerts touching rail trips/route_type=2: %d\n", railAlerts)
	if len(al.Entity) > 0 {
		fmt.Printf("sample alert:\n%s\n", prototext(al.Entity[0].Alert))
	}

	// trip id shape
	var ids []string
	for id := range tripIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Println("\n== sample realtime rail trip_ids ==")
	for i := 0; i < 5 && i < len(ids); i++ {
		fmt.Println(" ", ids[i])
	}
	fmt.Fprintln(os.Stderr, "done")
}

func prototext(m proto.Message) string {
	b, _ := (protoTextMarshal{}).Marshal(m)
	return string(b)
}

type protoTextMarshal struct{}

func (protoTextMarshal) Marshal(m proto.Message) ([]byte, error) {
	return []byte(fmt.Sprintf("%v", m)), nil
}
