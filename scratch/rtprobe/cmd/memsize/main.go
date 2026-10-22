package main

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"unsafe"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

// obs is a compact per-stop observation: what a de-duplicated history store
// actually needs to keep, as opposed to a whole decoded feed snapshot.
type obs struct {
	ArrTime  int64
	DepTime  int64
	ArrDelay int32
	DepDelay int32
	Seq      uint16
	Status   uint8
	_        uint8
}

func heap() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func main() {
	resp, err := http.Get("https://gtfsrt.api.translink.com.au/api/realtime/SEQ/TripUpdates/Rail")
	if err != nil {
		panic(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("wire bytes: %d\n", len(raw))

	const copies = 20
	base := heap()
	msgs := make([]*gtfs.FeedMessage, 0, copies)
	for range copies {
		var m gtfs.FeedMessage
		if err := proto.Unmarshal(raw, &m); err != nil {
			panic(err)
		}
		msgs = append(msgs, &m)
	}
	after := heap()
	per := (after - base) / copies
	fmt.Printf("decoded protobuf snapshot: %d bytes (%.1fx wire)\n", per, float64(per)/float64(len(raw)))

	nTrips, nSTU := 0, 0
	for _, e := range msgs[0].Entity {
		if e.TripUpdate != nil {
			nTrips++
			nSTU += len(e.TripUpdate.StopTimeUpdate)
		}
	}
	obsSize := unsafe.Sizeof(obs{})
	fmt.Printf("entities: %d  stop_time_updates: %d\n", nTrips, nSTU)
	fmt.Printf("obs struct: %d bytes/stop\n", obsSize)

	const (
		pollSec      = 15
		railDayStops = 248021 // measured: rail rows in stop_times.txt
		railDayTrips = 9919   // measured: rail trips in trips.txt
		operatingHrs = 19.0
	)
	for _, h := range []int{1, 3, 24} {
		snaps := uint64(h) * 3600 / pollSec
		frac := min(float64(h)/operatingHrs, 1)
		stops := railDayStops * frac
		trips := railDayTrips * frac
		dedup := stops*float64(obsSize) + trips*120 // + map entry and slice header overhead
		fmt.Printf("\n-- %dh window (%d polls @%ds) --\n", h, snaps, pollSec)
		fmt.Printf("  naive raw-snapshot ring : %8.1f MB\n", float64(snaps*per)/1e6)
		fmt.Printf("  de-duplicated per-trip  : %8.1f MB  (%.0f stop observations, %.0f trips)\n",
			dedup/1e6, stops, trips)
	}
	runtime.KeepAlive(msgs)
}
