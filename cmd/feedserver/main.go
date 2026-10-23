// Command feedserver serves the committed fixtures in the same layout as
// Translink's endpoints, so `trains serve` can run fully offline:
//
//	go run ./cmd/feedserver -addr localhost:8091
//	TRAINS_NOW=2026-10-23T00:04:00+10:00 trains serve \
//	    --static-url http://localhost:8091/GTFS/SEQ_GTFS.zip \
//	    --feeds-url  http://localhost:8091/api/realtime/SEQ
//
// The realtime capture and timetable are dated, so pair it with TRAINS_NOW
// set to the capture time. Responses carry ETags, so conditional GETs work.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	addr := flag.String("addr", "localhost:8091", "listen address")
	static := flag.String("static", "testdata/static.zip", "static GTFS zip to serve")
	rt := flag.String("rt", "testdata/rt/20261023T000334", "directory holding tu.pb, vp.pb and alerts.pb")
	flag.Parse()

	routes := map[string]string{
		"/GTFS/SEQ_GTFS.zip":                      *static,
		"/api/realtime/SEQ/TripUpdates/Rail":      filepath.Join(*rt, "tu.pb"),
		"/api/realtime/SEQ/VehiclePositions/Rail": filepath.Join(*rt, "vp.pb"),
		"/api/realtime/SEQ/Alerts":                filepath.Join(*rt, "alerts.pb"),
	}
	mux := http.NewServeMux()
	for route, file := range routes {
		body, err := os.ReadFile(file)
		if err != nil {
			log.Fatalf("feedserver: %v", err)
		}
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		mux.HandleFunc("GET "+route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("ETag", etag)
			http.ServeContent(w, r, filepath.Base(file), time.Time{}, bytes.NewReader(body))
		})
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("feedserver: %v", err)
	}
	fmt.Println("feedserver listening on http://" + ln.Addr().String())
	log.Fatal(http.Serve(ln, mux))
}
