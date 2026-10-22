// Command mkfixture cuts a small, real GTFS fixture out of the full Translink
// SEQ feed, and exports the rail station list.
//
//	go run ./cmd/mkfixture -in scratch/SEQ_GTFS.zip
//
// The fixture keeps every rail trip of the chosen service IDs with all their
// stops, calendar rows and shapes. IDs and field values are preserved
// verbatim; only shape points are thinned, to keep the fixture small.
package main

import (
	"archive/zip"
	"cmp"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

func main() {
	in := flag.String("in", "scratch/SEQ_GTFS.zip", "full SEQ GTFS zip")
	out := flag.String("out", "testdata/static.zip", "fixture zip to write")
	stations := flag.String("stations", "testdata/stations.csv", "rail station list to write (empty to skip)")
	services := flag.String("services", "QR 26_27-44634-0001000,QR 26_27-44635", "comma-separated service IDs to keep")
	thin := flag.Int("thin", 4, "keep every Nth shape point (endpoints always kept)")
	flag.Parse()
	if err := run(*in, *out, *stations, strings.Split(*services, ","), *thin); err != nil {
		fmt.Fprintln(os.Stderr, "mkfixture:", err)
		os.Exit(1)
	}
}

// table is a CSV file held as its header plus raw records.
type table struct {
	header []string
	col    map[string]int
	rows   [][]string
}

func (t *table) get(row []string, name string) string {
	i, ok := t.col[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func readTable(z *zip.Reader, name string, keep func(t *table, row []string) bool) (*table, error) {
	f, err := z.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s header: %w", name, err)
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	t := &table{header: header, col: map[string]int{}}
	for i, h := range header {
		t.col[strings.TrimSpace(h)] = i
	}
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if keep == nil || keep(t, row) {
			t.rows = append(t.rows, row)
		}
	}
	return t, nil
}

func run(in, out, stationsPath string, services []string, thin int) error {
	zr, err := zip.OpenReader(in)
	if err != nil {
		return err
	}
	defer zr.Close()
	z := &zr.Reader

	keepService := map[string]bool{}
	for _, s := range services {
		keepService[strings.TrimSpace(s)] = true
	}

	routes, err := readTable(z, "routes.txt", func(t *table, r []string) bool { return t.get(r, "route_type") == "2" })
	if err != nil {
		return err
	}
	railRoute := map[string]bool{}
	for _, r := range routes.rows {
		railRoute[routes.get(r, "route_id")] = true
	}

	allTrips, err := readTable(z, "trips.txt", func(t *table, r []string) bool { return railRoute[t.get(r, "route_id")] })
	if err != nil {
		return err
	}
	railTrip, keepTrip := map[string]bool{}, map[string]bool{}
	usedRoute, usedShape := map[string]bool{}, map[string]bool{}
	trips := &table{header: allTrips.header, col: allTrips.col}
	for _, r := range allTrips.rows {
		id := allTrips.get(r, "trip_id")
		railTrip[id] = true
		if keepService[allTrips.get(r, "service_id")] {
			keepTrip[id] = true
			usedRoute[allTrips.get(r, "route_id")] = true
			usedShape[allTrips.get(r, "shape_id")] = true
			trips.rows = append(trips.rows, r)
		}
	}
	if len(trips.rows) == 0 {
		return fmt.Errorf("no rail trips for services %v", services)
	}

	railStop, fixtureStop := map[string]bool{}, map[string]bool{}
	stopTimes, err := readTable(z, "stop_times.txt", func(t *table, r []string) bool {
		trip := t.get(r, "trip_id")
		if !railTrip[trip] {
			return false
		}
		stop := t.get(r, "stop_id")
		railStop[stop] = true
		if keepTrip[trip] {
			fixtureStop[stop] = true
			return true
		}
		return false
	})
	if err != nil {
		return err
	}

	allStops, err := readTable(z, "stops.txt", nil)
	if err != nil {
		return err
	}
	railParent, fixtureParent := map[string]bool{}, map[string]bool{}
	for _, r := range allStops.rows {
		id, parent := allStops.get(r, "stop_id"), allStops.get(r, "parent_station")
		if railStop[id] && parent != "" {
			railParent[parent] = true
		}
		if fixtureStop[id] && parent != "" {
			fixtureParent[parent] = true
		}
	}
	stops := &table{header: allStops.header, col: allStops.col}
	var stationRows [][]string
	for _, r := range allStops.rows {
		id, parent := allStops.get(r, "stop_id"), allStops.get(r, "parent_station")
		// Keep the fixture's stations and all of their platforms.
		if fixtureParent[id] || fixtureParent[parent] {
			stops.rows = append(stops.rows, r)
		}
		if railParent[id] {
			stationRows = append(stationRows, []string{
				id, allStops.get(r, "stop_name"), allStops.get(r, "stop_lat"), allStops.get(r, "stop_lon"),
			})
		}
	}

	routesOut := &table{header: routes.header, col: routes.col}
	for _, r := range routes.rows {
		if usedRoute[routes.get(r, "route_id")] {
			routesOut.rows = append(routesOut.rows, r)
		}
	}

	byService := func(t *table, r []string) bool { return keepService[t.get(r, "service_id")] }
	calendar, err := readTable(z, "calendar.txt", byService)
	if err != nil {
		return err
	}
	calendarDates, err := readTable(z, "calendar_dates.txt", byService)
	if err != nil {
		return err
	}
	agency, err := readTable(z, "agency.txt", nil)
	if err != nil {
		return err
	}
	feedInfo, err := readTable(z, "feed_info.txt", nil)
	if err != nil {
		return err
	}

	shapes, err := readTable(z, "shapes.txt", func(t *table, r []string) bool { return usedShape[t.get(r, "shape_id")] })
	if err != nil {
		return err
	}
	shapes.rows = thinShapes(shapes, thin)

	if stationsPath != "" {
		slices.SortFunc(stationRows, func(a, b []string) int { return cmp.Compare(a[1], b[1]) })
		if err := writeCSVFile(stationsPath, []string{"id", "name", "lat", "lon"}, stationRows); err != nil {
			return err
		}
		fmt.Printf("%s: %d rail stations\n", stationsPath, len(stationRows))
	}

	files := []struct {
		name string
		t    *table
	}{
		{"agency.txt", agency}, {"feed_info.txt", feedInfo}, {"routes.txt", routesOut},
		{"trips.txt", trips}, {"stop_times.txt", stopTimes}, {"stops.txt", stops},
		{"calendar.txt", calendar}, {"calendar_dates.txt", calendarDates}, {"shapes.txt", shapes},
	}
	if err := writeZip(out, files); err != nil {
		return err
	}
	fmt.Printf("%s: %d trips, %d stop_times, %d stops, %d shape points, services %v\n",
		out, len(trips.rows), len(stopTimes.rows), len(stops.rows), len(shapes.rows), services)
	return nil
}

// thinShapes keeps every nth point of each shape plus its first and last.
func thinShapes(t *table, n int) [][]string {
	if n <= 1 {
		return t.rows
	}
	var out [][]string
	for i, r := range t.rows {
		id := t.get(r, "shape_id")
		first := i == 0 || t.get(t.rows[i-1], "shape_id") != id
		last := i == len(t.rows)-1 || t.get(t.rows[i+1], "shape_id") != id
		if first || last || i%n == 0 {
			out = append(out, r)
		}
	}
	return out
}

func writeZip(path string, files []struct {
	name string
	t    *table
}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, file := range files {
		w, err := zw.Create(file.name)
		if err != nil {
			return err
		}
		if err := writeCSV(w, file.t.header, file.t.rows); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

func writeCSVFile(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := writeCSV(f, header, rows); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeCSV(w io.Writer, header []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true // match the upstream feed
	if err := cw.Write(header); err != nil {
		return err
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}
