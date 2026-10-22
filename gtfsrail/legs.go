package gtfsrail

import (
	"strings"
	"time"

	"trains/gtfsrail/internal/gtfs"
	"trains/gtfsrail/internal/gtfsrt"
)

// query holds one consistent view of the static index and realtime snapshot
// for the duration of a single request.
type query struct {
	st   *state
	snap *gtfsrt.Snapshot
	rt   realtime
	now  time.Time
	obs  map[string]map[uint16]gtfsrt.Obs
	legs map[[2]StationID][]Leg // direct legs memoised per (origin, destination)
	at   time.Time
}

func (s *Service) newQuery(at time.Time) *query {
	now := s.now()
	if at.IsZero() {
		at = now
	}
	return &query{
		st: s.cur.Load(), snap: s.rt.Snapshot(), rt: s.rt, now: now, at: at,
		obs: map[string]map[uint16]gtfsrt.Obs{}, legs: map[[2]StationID][]Leg{},
	}
}

func (q *query) observations(tripID string) map[uint16]gtfsrt.Obs {
	o, ok := q.obs[tripID]
	if !ok {
		o = q.rt.Observations(tripID)
		q.obs[tripID] = o
	}
	return o
}

// rawCall is a call before realtime is applied.
type rawCall struct {
	stopID             string
	seq                uint16
	schedArr, schedDep time.Time
}

func secs(s gtfs.Secs) time.Duration { return time.Duration(s) * time.Second }

// staticRaw lists a static trip's calls on a service date.
func staticRaw(ix *gtfs.Index, t *gtfs.Trip, date Date) []rawCall {
	midnight := date.Midnight(Brisbane)
	sts := ix.TripStopTimes(t)
	raws := make([]rawCall, len(sts))
	for i, st := range sts {
		raws[i] = rawCall{
			stopID: st.StopID, seq: st.Seq,
			schedArr: midnight.Add(secs(st.Arrival)), schedDep: midnight.Add(secs(st.Departure)),
		}
	}
	return raws
}

// realtimeApplies reports whether realtime data for a trip describes the run
// on this service date. Trip IDs repeat across the days a service runs, so the
// instance must be matched.
//
// Translink stamps a trip that starts after midnight with the calendar date
// and a sub-24h start_time (start_date 20261023, start_time 00:02:00 for a
// trip scheduled at 24:02:00 on the 22nd's service). Comparing start dates
// alone would discard its realtime, or attach it to the wrong day's run, so
// the start instant is compared instead. Runs of one trip are 24h apart, so
// the nearest instance within 12h is unambiguous. Without a start_date the
// update is accepted when the run is plausibly in progress.
func (q *query) realtimeApplies(tu *gtfsrt.TripUpdate, date Date, raws []rawCall) bool {
	if tu != nil && tu.StartDate != "" {
		if tu.StartTime != "" && len(raws) > 0 {
			sd, errD := ParseDate(tu.StartDate)
			st, errT := gtfs.ParseSecs(tu.StartTime)
			if errD == nil && errT == nil {
				d := sd.Midnight(Brisbane).Add(secs(st)).Sub(raws[0].schedDep)
				return d > -12*time.Hour && d < 12*time.Hour
			}
		}
		return tu.StartDate == date.GTFS()
	}
	if len(raws) == 0 {
		return false
	}
	from := raws[0].schedDep.Add(-3 * time.Hour)
	to := raws[len(raws)-1].schedArr.Add(3 * time.Hour)
	return !q.now.Before(from) && !q.now.After(to)
}

// staticLeg builds a leg for a scheduled trip, with realtime overlaid.
func (q *query) staticLeg(t *gtfs.Trip, date Date, board, alight int) Leg {
	raws := staticRaw(q.st.ix, t, date)
	tu := q.snap.Trips[t.ID]
	return q.build(legInput{
		tripID: t.ID, routeID: t.RouteID, shapeID: t.ShapeID, headsign: t.Headsign,
		date: date, raws: raws, tu: tu, applies: q.realtimeApplies(tu, date, raws),
		board: board, alight: alight,
	})
}

// addedLeg synthesises a leg for an ADDED trip, which has no static
// counterpart, purely from its stop time updates.
func (q *query) addedLeg(tu *gtfsrt.TripUpdate) (Leg, bool) {
	date, err := ParseDate(tu.StartDate)
	if err != nil {
		date = DateOf(q.now, Brisbane)
	}
	var raws []rawCall
	for _, u := range tu.Updates {
		if !u.HasSeq {
			continue
		}
		arr, okA := eventTime(u.Arrival, u.HasArrival, time.Time{})
		dep, okD := eventTime(u.Departure, u.HasDeparture, time.Time{})
		if !okA && !okD {
			continue
		}
		if !okA {
			arr = dep
		}
		if !okD {
			dep = arr
		}
		// The scheduled time is the realtime time minus the reported delay.
		schedArr := arr.Add(-time.Duration(u.Arrival.Delay) * time.Second)
		schedDep := dep.Add(-time.Duration(u.Departure.Delay) * time.Second)
		if !u.HasArrival || !u.Arrival.HasDelay {
			schedArr = arr.Add(-time.Duration(u.Departure.Delay) * time.Second)
		}
		if !u.HasDeparture || !u.Departure.HasDelay {
			schedDep = dep.Add(-time.Duration(u.Arrival.Delay) * time.Second)
		}
		raws = append(raws, rawCall{stopID: u.StopID, seq: uint16(u.Seq), schedArr: schedArr, schedDep: schedDep})
	}
	if len(raws) == 0 {
		return Leg{}, false
	}
	leg := q.build(legInput{
		tripID: tu.TripID, routeID: tu.RouteID, date: date, raws: raws, tu: tu, applies: true,
		board: 0, alight: len(raws) - 1,
	})
	leg.Added = true
	if last := leg.Calls[len(leg.Calls)-1]; last.Name != "" {
		leg.Headsign = last.Name
	}
	return leg, true
}

// eventTime resolves a stop time event to an absolute time: the explicit time
// when present, else the scheduled time plus the delay.
func eventTime(e gtfsrt.Event, has bool, sched time.Time) (time.Time, bool) {
	if !has {
		return time.Time{}, false
	}
	if e.HasTime && e.Time != 0 {
		return time.Unix(e.Time, 0), true
	}
	if e.HasDelay && !sched.IsZero() {
		return sched.Add(time.Duration(e.Delay) * time.Second), true
	}
	return time.Time{}, false
}

type legInput struct {
	tripID, routeID, shapeID, headsign string
	date                               Date
	raws                               []rawCall
	tu                                 *gtfsrt.TripUpdate
	applies                            bool
	board, alight                      int
}

// build applies realtime to a trip's calls (SPEC.md §3.5).
func (q *query) build(in legInput) Leg {
	ix := q.st.ix
	n := len(in.raws)
	leg := Leg{
		TripID: TripID(in.tripID), ServiceDate: in.date, ShapeID: ShapeID(in.shapeID),
		Headsign: in.headsign, Calls: make([]Call, n), Board: in.board, Alight: in.alight,
	}
	if r := ix.Routes[in.routeID]; r != nil {
		leg.RouteName = r.LongName
		if r.Color != "" {
			leg.RouteColor = "#" + strings.TrimPrefix(r.Color, "#")
		}
	}

	tu := in.tu
	var obs map[uint16]gtfsrt.Obs
	var veh *gtfsrt.Vehicle
	if in.applies {
		obs = q.observations(in.tripID)
		veh = q.snap.Vehicles[in.tripID]
	} else {
		tu = nil
	}
	cancelled := tu != nil && (tu.Rel == gtfsrt.RelCanceled || tu.Rel == gtfsrt.RelDeleted)

	bySeq := map[uint32]*gtfsrt.StopTimeUpdate{}
	byStop := map[string]*gtfsrt.StopTimeUpdate{}
	if tu != nil {
		for i := range tu.Updates {
			u := &tu.Updates[i]
			if u.HasSeq {
				bySeq[u.Seq] = u
			} else if u.StopID != "" {
				byStop[u.StopID] = u
			}
		}
	}

	covered := make([]bool, n)   // a realtime time exists for this call
	projected := make([]bool, n) // time carried forward from an earlier call
	var carry time.Duration
	haveCarry := false
	for i, r := range in.raws {
		c := &leg.Calls[i]
		c.Sequence = r.seq
		c.Scheduled, c.scheduledArrival = r.schedDep, r.schedArr
		if i == n-1 {
			// A final call has no departure; show its arrival.
			c.Scheduled = r.schedArr
		}

		// Rules 1–3: observation table first, then the live stop time update
		// by sequence, then by stop ID.
		u := bySeq[uint32(r.seq)]
		if u == nil {
			u = byStop[r.stopID]
		}
		stopID := r.stopID
		if u != nil && u.AssignedStopID != "" {
			stopID = u.AssignedStopID
		}
		describeStop(ix, c, stopID)

		arr, dep, skipped, ok := realtimeTimes(obs, r, u)
		switch {
		case cancelled:
			c.Skipped = true
			c.Expected, c.expectedArrival = c.Scheduled, r.schedArr
		case skipped:
			// Rule 6: a skipped stop's times are unusable and it does not
			// reset the carried delay.
			c.Skipped = true
			c.Expected, c.expectedArrival = c.Scheduled, r.schedArr
			if haveCarry {
				c.Expected, c.expectedArrival = c.Scheduled.Add(carry), r.schedArr.Add(carry)
				projected[i] = true
			}
		case ok:
			c.expectedArrival = arr
			c.Expected = dep
			if i == n-1 {
				c.Expected = arr
			}
			if !dep.IsZero() && !r.schedDep.IsZero() {
				carry = dep.Sub(r.schedDep)
			} else {
				carry = arr.Sub(r.schedArr)
			}
			haveCarry = true
			covered[i] = true
		case haveCarry:
			// Rule 5: after the last update, the last observed delay carries.
			c.Expected, c.expectedArrival = c.Scheduled.Add(carry), r.schedArr.Add(carry)
			projected[i] = true
		default:
			// Rule 4: before the first update, the schedule stands.
			c.Expected, c.expectedArrival = c.Scheduled, r.schedArr
		}
	}

	// Position: a fresh vehicle fix is authoritative; otherwise time decides.
	cur := n
	if veh != nil {
		leg.Vehicle = &VehicleFix{
			Lat: veh.Lat, Lon: veh.Lon, AtSequence: uint16(veh.Seq), Status: veh.Status.String(),
			ObservedAt: veh.Timestamp, Stale: veh.Stale,
		}
	}
	if veh != nil && !veh.Stale {
		for i, c := range leg.Calls {
			if uint32(c.Sequence) >= veh.Seq {
				cur = i
				break
			}
		}
	} else {
		for i := range leg.Calls {
			t := leg.Calls[i].Expected
			if !t.Before(q.now) {
				cur = i
				break
			}
		}
	}

	for i := range leg.Calls {
		c := &leg.Calls[i]
		switch {
		case i < cur:
			c.Position = Passed
		case i == cur:
			c.Position = Current
		default:
			c.Position = Upcoming
		}
		switch {
		case c.Position == Passed && covered[i]:
			c.Source = Observed
			actual := c.Expected
			c.Actual = &actual
		case c.Position == Passed:
			// Not observed: show the schedule, never an extrapolation.
			c.Source = Scheduled
			c.Expected, c.expectedArrival = c.Scheduled, c.scheduledArrival
		case covered[i]:
			c.Source = Predicted
		case projected[i]:
			c.Source = Projected
		default:
			c.Source = Scheduled
		}
	}

	leg.RunNumber = runNumber(in.tripID, tu, veh)
	leg.Alerts, leg.Punctuality = q.alertsAndPunctuality(&leg, in.routeID, cancelled)
	return leg
}

// realtimeTimes returns the realtime arrival and departure for a call from the
// observation table (preferred) or the live stop time update.
func realtimeTimes(obs map[uint16]gtfsrt.Obs, r rawCall, u *gtfsrt.StopTimeUpdate) (arr, dep time.Time, skipped, ok bool) {
	if o, has := obs[r.seq]; has {
		if o.Skipped {
			return time.Time{}, time.Time{}, true, false
		}
		arr = obsTime(o.ArrTime, o.ArrDelay, r.schedArr)
		dep = obsTime(o.DepTime, o.DepDelay, r.schedDep)
	} else if u != nil {
		if u.Skipped {
			return time.Time{}, time.Time{}, true, false
		}
		if u.NoData {
			return time.Time{}, time.Time{}, false, false
		}
		arr, _ = eventTime(u.Arrival, u.HasArrival, r.schedArr)
		dep, _ = eventTime(u.Departure, u.HasDeparture, r.schedDep)
	}
	switch {
	case arr.IsZero() && dep.IsZero():
		return time.Time{}, time.Time{}, false, false
	case arr.IsZero():
		arr = dep
	case dep.IsZero():
		dep = arr
	}
	if dep.Before(arr) {
		dep = arr
	}
	return arr, dep, false, true
}

func obsTime(unix int64, delay int32, sched time.Time) time.Time {
	if unix != 0 {
		return time.Unix(unix, 0)
	}
	if delay != 0 && !sched.IsZero() {
		return sched.Add(time.Duration(delay) * time.Second)
	}
	return time.Time{}
}

// describeStop fills station, name and platform for the stop actually used.
func describeStop(ix *gtfs.Index, c *Call, stopID string) {
	c.Stop = StopID(stopID)
	if stop := ix.Stops[stopID]; stop != nil {
		c.Platform = stop.Platform
		if st := ix.Stations[stop.Parent]; st != nil {
			c.Station, c.Name = StationID(st.ID), st.Name
			return
		}
	}
	if st := ix.Stations[stopID]; st != nil {
		c.Station, c.Name = StationID(st.ID), st.Name
		return
	}
	c.Name = "Unknown stop " + stopID
}

// runNumber is the QR run number: the vehicle label when reported, which in
// the live feed always equals the trip ID suffix (SPEC.md §2.3).
func runNumber(tripID string, tu *gtfsrt.TripUpdate, veh *gtfsrt.Vehicle) string {
	if tu != nil && tu.VehicleLabel != "" {
		return tu.VehicleLabel
	}
	if veh != nil && veh.Label != "" {
		return veh.Label
	}
	if i := strings.LastIndexByte(tripID, '-'); i >= 0 && i < len(tripID)-1 {
		return tripID[i+1:]
	}
	return ""
}

// alertsAndPunctuality collects active alerts for the leg and classifies it.
func (q *query) alertsAndPunctuality(leg *Leg, routeID string, cancelled bool) ([]Alert, Punctuality) {
	var out []Alert
	seen := map[string]bool{}
	noService := false
	add := func(as []*gtfsrt.Alert, tripLevel bool) {
		for _, a := range as {
			active, until := a.ActiveAt(q.now)
			if !active {
				continue
			}
			if tripLevel && a.Effect == "NO_SERVICE" {
				noService = true
			}
			if seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			al := Alert{ID: a.ID, Header: a.Header, Description: a.Description, Effect: a.Effect, Cause: a.Cause}
			if !until.IsZero() {
				al.ActiveUntil = &until
			}
			out = append(out, al)
		}
	}
	add(q.snap.AlertsByTrip[string(leg.TripID)], true)
	add(q.snap.AlertsByRoute[routeID], false)
	for _, i := range []int{leg.Board, leg.Alight} {
		c := leg.Calls[i]
		add(q.snap.AlertsByStop[string(c.Stop)], false)
		if c.Station != "" {
			add(q.snap.AlertsByStop[string(c.Station)], false)
		}
	}

	b := &leg.Calls[leg.Board]
	switch {
	case cancelled || noService:
		return out, Cancelled
	case b.Skipped:
		return out, Skipping
	case b.Source == Scheduled:
		return out, NoRealtime
	}
	leg.DelaySeconds = int32(b.Expected.Sub(b.Scheduled) / time.Second)
	return out, Classify(leg.DelaySeconds)
}
