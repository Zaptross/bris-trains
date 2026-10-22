package gtfs

import (
	"archive/zip"
	"cmp"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"trains/gtfsrail/internal/geo"
)

// ParseFile parses the GTFS zip at path.
func ParseFile(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open gtfs zip: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat gtfs zip: %w", err)
	}
	ix, err := Parse(f, fi.Size())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ix, nil
}

// Parse reads a GTFS zip and builds the rail index in one streaming pass per
// file, in dependency order: routes → trips → stops → stop_times → shapes →
// calendar, calendar_dates, feed_info. Only rail rows are retained; the 213 MB
// stop_times.txt is streamed, never held.
//
// A missing required file or column is an error. Individual unparsable rows
// are skipped and counted, with one warning per file via slog.Default.
func Parse(r io.ReaderAt, size int64) (*Index, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("open gtfs zip: %w", err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		// Tolerate zips that nest the feed in one directory.
		if _, dup := files[path.Base(f.Name)]; !dup {
			files[path.Base(f.Name)] = f
		}
	}

	p := &parser{files: files, ix: &Index{}}
	steps := []func() error{
		p.routes, p.trips, p.stops, p.stopTimes, p.platforms,
		p.shapes, p.calendar, p.calendarDates, p.feedInfo,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return p.ix, nil
}

// parser carries state between the per-file passes.
type parser struct {
	files map[string]*zip.File
	ix    *Index

	services   map[string]bool     // rail service IDs
	railShapes map[string]bool     // rail shape IDs
	allStops   map[string]*rawStop // every stop in stops.txt
	otherStops map[string]struct{} // stops called at by non-rail trips
	usedStops  map[string]struct{} // stops called at by rail trips
}

type rawStop struct {
	stop     Stop
	locType  string
	parentID string
}

// skipped logs the per-file count of rows that could not be used.
func skipped(file string, rows int) {
	if rows > 0 {
		slog.Default().Warn("gtfs: skipped unparsable rows", "file", file, "rows", rows)
	}
}

// each opens a table, calls row for every record, and closes it. row reports
// false to count the record as skipped.
func (p *parser) each(name string, optional bool, required []string, setup func(t *table), row func(t *table) bool) error {
	t, err := openTable(p.files, name, required...)
	if errors.Is(err, errMissingFile) && optional {
		return nil
	}
	if err != nil {
		return err
	}
	defer t.close()
	setup(t)
	bad := 0
	for {
		ok, err := t.next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if !row(t) {
			bad++
		}
	}
	skipped(name, bad+t.bad)
	return nil
}

func (p *parser) routes() error {
	p.ix.Routes = make(map[string]*Route)
	var id, short, long, typ, color, text int
	return p.each("routes.txt", false, []string{"route_id", "route_type"}, func(t *table) {
		id, short, long, typ = t.col("route_id"), t.col("route_short_name"), t.col("route_long_name"), t.col("route_type")
		color, text = t.col("route_color"), t.col("route_text_color")
	}, func(t *table) bool {
		if t.get(typ) != "2" {
			return true
		}
		r := &Route{
			ID:        strings.Clone(t.get(id)),
			ShortName: strings.Clone(t.get(short)),
			LongName:  strings.Clone(t.get(long)),
			Color:     strings.Clone(t.get(color)),
			TextColor: strings.Clone(t.get(text)),
		}
		if r.ID == "" {
			return false
		}
		p.ix.Routes[r.ID] = r
		return true
	})
}

func (p *parser) trips() error {
	p.ix.Trips = make(map[string]*Trip)
	p.services = make(map[string]bool)
	p.railShapes = make(map[string]bool)
	var route, svc, id, head, dir, shape int
	return p.each("trips.txt", false, []string{"route_id", "service_id", "trip_id"}, func(t *table) {
		route, svc, id = t.col("route_id"), t.col("service_id"), t.col("trip_id")
		head, dir, shape = t.col("trip_headsign"), t.col("direction_id"), t.col("shape_id")
	}, func(t *table) bool {
		r, ok := p.ix.Routes[t.get(route)]
		if !ok {
			return true
		}
		tr := &Trip{
			ID:        strings.Clone(t.get(id)),
			RouteID:   r.ID,
			ServiceID: strings.Clone(t.get(svc)),
			ShapeID:   strings.Clone(t.get(shape)),
			Headsign:  strings.Clone(t.get(head)),
		}
		if tr.ID == "" || tr.ServiceID == "" {
			return false
		}
		if t.get(dir) == "1" {
			tr.Direction = 1
		}
		p.ix.Trips[tr.ID] = tr
		p.services[tr.ServiceID] = true
		if tr.ShapeID != "" {
			p.railShapes[tr.ShapeID] = true
		}
		return true
	})
}

func (p *parser) stops() error {
	p.allStops = make(map[string]*rawStop)
	var id, code, name, lat, lon, loc, parent, plat int
	return p.each("stops.txt", false, []string{"stop_id"}, func(t *table) {
		id, code, name = t.col("stop_id"), t.col("stop_code"), t.col("stop_name")
		lat, lon, loc = t.col("stop_lat"), t.col("stop_lon"), t.col("location_type")
		parent, plat = t.col("parent_station"), t.col("platform_code")
	}, func(t *table) bool {
		la, err1 := strconv.ParseFloat(t.get(lat), 64)
		lo, err2 := strconv.ParseFloat(t.get(lon), 64)
		sid := t.get(id)
		if sid == "" || err1 != nil || err2 != nil {
			return false
		}
		sid = strings.Clone(sid)
		p.allStops[sid] = &rawStop{
			stop: Stop{
				ID:       sid,
				Code:     strings.Clone(t.get(code)),
				Name:     strings.Clone(t.get(name)),
				Lat:      la,
				Lon:      lo,
				Platform: strings.Clone(t.get(plat)),
			},
			locType:  strings.Clone(t.get(loc)),
			parentID: strings.Clone(t.get(parent)),
		}
		return true
	})
}

// tripStop is a rail stop time awaiting placement in the flat array.
type tripStop struct {
	trip int32 // ordinal in tripOrder
	st   StopTime
}

func (p *parser) stopTimes() error {
	tripOrder := slices.Sorted(maps.Keys(p.ix.Trips))
	ordinal := make(map[string]int32, len(tripOrder))
	for i, id := range tripOrder {
		ordinal[id] = int32(i)
	}
	p.otherStops = make(map[string]struct{})
	p.usedStops = make(map[string]struct{})
	var rows []tripStop

	var trip, stop, seq, arr, dep, pick, drop int
	err := p.each("stop_times.txt", false,
		[]string{"trip_id", "stop_id", "stop_sequence", "arrival_time", "departure_time"},
		func(t *table) {
			trip, stop, seq = t.col("trip_id"), t.col("stop_id"), t.col("stop_sequence")
			arr, dep = t.col("arrival_time"), t.col("departure_time")
			pick, drop = t.col("pickup_type"), t.col("drop_off_type")
		}, func(t *table) bool {
			sid := t.get(stop)
			n, rail := ordinal[t.get(trip)]
			if !rail {
				if _, seen := p.otherStops[sid]; !seen && sid != "" {
					p.otherStops[strings.Clone(sid)] = struct{}{}
				}
				return true
			}
			rs, ok := p.allStops[sid]
			if !ok {
				return false
			}
			sq, err := strconv.ParseUint(t.get(seq), 10, 16)
			if err != nil {
				return false
			}
			a, d, ok := parseTimes(t.get(arr), t.get(dep))
			if !ok {
				return false
			}
			pt, ok1 := parseSmall(t.get(pick))
			dt, ok2 := parseSmall(t.get(drop))
			if !ok1 || !ok2 {
				return false
			}
			rows = append(rows, tripStop{trip: n, st: StopTime{
				StopID:      rs.stop.ID, // interned
				Seq:         uint16(sq),
				Arrival:     a,
				Departure:   d,
				PickupType:  pt,
				DropOffType: dt,
			}})
			p.usedStops[rs.stop.ID] = struct{}{}
			return true
		})
	if err != nil {
		return err
	}

	slices.SortFunc(rows, func(a, b tripStop) int {
		return cmp.Or(cmp.Compare(a.trip, b.trip), cmp.Compare(a.st.Seq, b.st.Seq))
	})
	p.ix.StopTimes = make([]StopTime, len(rows))
	for i, r := range rows {
		p.ix.StopTimes[i] = r.st
		tr := p.ix.Trips[tripOrder[r.trip]]
		if tr.Count == 0 {
			tr.First = int32(i)
		}
		tr.Count++
	}
	return nil
}

// parseTimes parses arrival and departure, letting either stand in for a
// missing other (GTFS permits blanks on non-timepoints only in pairs, but the
// fallback costs nothing).
func parseTimes(arr, dep string) (Secs, Secs, bool) {
	if arr == "" {
		arr = dep
	}
	if dep == "" {
		dep = arr
	}
	a, err1 := ParseSecs(arr)
	d, err2 := ParseSecs(dep)
	return a, d, err1 == nil && err2 == nil
}

// parseSmall parses an optional small enum field; blank means 0.
func parseSmall(s string) (uint8, bool) {
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseUint(s, 10, 8)
	return uint8(v), err == nil
}

// platforms derives Stations, Stops and Calling from the stops rail trips use.
func (p *parser) platforms() error {
	ix := p.ix
	ix.Stations = make(map[string]*Station)
	orphans := 0
	for id := range p.usedStops {
		rs := p.allStops[id]
		pid := rs.parentID
		if pid == "" {
			// A rail stop with no parent becomes its own station.
			orphans++
			pid = id
		}
		if _, ok := ix.Stations[pid]; ok {
			continue
		}
		st := &Station{ID: pid, Name: rs.stop.Name, Lat: rs.stop.Lat, Lon: rs.stop.Lon}
		if parent, ok := p.allStops[pid]; ok {
			st.ID, st.Name, st.Lat, st.Lon = parent.stop.ID, parent.stop.Name, parent.stop.Lat, parent.stop.Lon
		} else {
			orphans++
		}
		ix.Stations[pid] = st
	}
	if orphans > 0 {
		slog.Default().Warn("gtfs: rail stops without a usable parent station", "stops", orphans)
	}

	ix.Stops = make(map[string]*Stop)
	for id, rs := range p.allStops {
		pid := rs.parentID
		_, used := p.usedStops[id]
		if !used {
			_, other := p.otherStops[id]
			platform := rs.locType == "" || rs.locType == "0"
			if _, rail := ix.Stations[pid]; !rail || other || !platform {
				continue
			}
		}
		if pid == "" {
			pid = id
		}
		s := rs.stop
		s.Parent = pid
		ix.Stops[id] = &s
		ix.Stations[pid].Platforms = append(ix.Stations[pid].Platforms, s.ID)
	}
	for _, st := range ix.Stations {
		slices.Sort(st.Platforms)
	}
	p.allStops, p.otherStops, p.usedStops = nil, nil, nil
	ix.buildCalling()
	return nil
}

func (p *parser) shapes() error {
	raw := make(map[string][]shapePt)
	var id, lat, lon, seq int
	err := p.each("shapes.txt", true, []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}, func(t *table) {
		id, lat, lon, seq = t.col("shape_id"), t.col("shape_pt_lat"), t.col("shape_pt_lon"), t.col("shape_pt_sequence")
	}, func(t *table) bool {
		sid := t.get(id)
		if !p.railShapes[sid] {
			return true
		}
		la, err1 := strconv.ParseFloat(t.get(lat), 64)
		lo, err2 := strconv.ParseFloat(t.get(lon), 64)
		sq, err3 := strconv.ParseUint(t.get(seq), 10, 32)
		if err1 != nil || err2 != nil || err3 != nil {
			return false
		}
		if _, ok := raw[sid]; !ok {
			sid = strings.Clone(sid)
		}
		raw[sid] = append(raw[sid], shapePt{seq: uint32(sq), p: geo.Point{Lat: la, Lon: lo}})
		return true
	})
	if err != nil {
		return err
	}
	p.ix.Shapes = make(map[string][]geo.Point, len(raw))
	pts := []geo.Point{}
	for sid, sp := range raw {
		slices.SortStableFunc(sp, func(a, b shapePt) int { return cmp.Compare(a.seq, b.seq) })
		pts = pts[:0]
		for _, s := range sp {
			pts = append(pts, s.p)
		}
		p.ix.Shapes[sid] = geo.Simplify(pts, MaxShapePoints)
	}
	return nil
}

type shapePt struct {
	seq uint32
	p   geo.Point
}

func (p *parser) calendar() error {
	p.ix.Calendars = make(map[string]Calendar)
	dayCols := [7]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}
	var id, start, end int
	var days [7]int
	required := append([]string{"service_id", "start_date", "end_date"}, dayCols[:]...)
	return p.each("calendar.txt", true, required, func(t *table) {
		id, start, end = t.col("service_id"), t.col("start_date"), t.col("end_date")
		for i, c := range dayCols {
			days[i] = t.col(c)
		}
	}, func(t *table) bool {
		sid := t.get(id)
		if !p.services[sid] {
			return true
		}
		c := Calendar{Start: strings.Clone(t.get(start)), End: strings.Clone(t.get(end))}
		if !validDate(c.Start) || !validDate(c.End) {
			return false
		}
		for i, col := range days {
			c.Days[i] = t.get(col) == "1"
		}
		p.ix.Calendars[strings.Clone(sid)] = c
		return true
	})
}

func (p *parser) calendarDates() error {
	p.ix.CalendarDates = make(map[string]map[string]uint8)
	var id, date, typ int
	return p.each("calendar_dates.txt", true, []string{"service_id", "date", "exception_type"}, func(t *table) {
		id, date, typ = t.col("service_id"), t.col("date"), t.col("exception_type")
	}, func(t *table) bool {
		sid := t.get(id)
		if !p.services[sid] {
			return true
		}
		d, et := t.get(date), t.get(typ)
		if !validDate(d) || (et != "1" && et != "2") {
			return false
		}
		m, ok := p.ix.CalendarDates[sid]
		if !ok {
			m = make(map[string]uint8)
			p.ix.CalendarDates[strings.Clone(sid)] = m
		}
		m[strings.Clone(d)] = et[0] - '0'
		return true
	})
}

func (p *parser) feedInfo() error {
	var start, end int
	return p.each("feed_info.txt", true, nil, func(t *table) {
		start, end = t.col("feed_start_date"), t.col("feed_end_date")
	}, func(t *table) bool {
		p.ix.FeedStart, p.ix.FeedEnd = strings.Clone(t.get(start)), strings.Clone(t.get(end))
		return true
	})
}

// validDate reports whether s is an 8-digit YYYYMMDD string.
func validDate(s string) bool {
	if len(s) != 8 {
		return false
	}
	_, ok := digits(s)
	return ok
}
