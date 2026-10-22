package gtfsrail

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"trains/gtfsrail/internal/gtfs"
	"trains/gtfsrail/internal/gtfsrt"
)

// JourneyOptions tunes Journeys.
type JourneyOptions struct {
	// Limit is the number of journeys to return; default 5.
	Limit int
	// DirectOnly suppresses one-change journeys.
	DirectOnly bool
}

// ErrSameStation is returned when origin and destination are the same.
var ErrSameStation = errors.New("origin and destination are the same station")

// searchHorizon bounds how far ahead journeys are looked for. It spans a full
// service day so that late-night queries still find the first morning train.
const searchHorizon = 26 * time.Hour

// Journeys returns the next journeys from origin to destination departing at
// or after at (now when zero): direct trains, plus one-change journeys that
// beat them (SPEC.md §3.5, §3.6).
func (s *Service) Journeys(ctx context.Context, origin, dest StationID, at time.Time, opt JourneyOptions) ([]Journey, Freshness, error) {
	q := s.newQuery(at)
	fresh := s.freshness(q.snap, q.now)
	if _, ok := q.st.byID[origin]; !ok {
		return nil, fresh, fmt.Errorf("origin %q: %w", origin, ErrNotFound)
	}
	if _, ok := q.st.byID[dest]; !ok {
		return nil, fresh, fmt.Errorf("destination %q: %w", dest, ErrNotFound)
	}
	if origin == dest {
		return nil, fresh, ErrSameStation
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = 5
	}

	direct := q.direct(origin, dest)
	var journeys []Journey
	for _, l := range direct {
		if alightable(&l) {
			journeys = append(journeys, newJourney([]Leg{l}, nil))
		}
	}
	if !opt.DirectOnly {
		if err := ctx.Err(); err != nil {
			return nil, fresh, err
		}
		journeys = append(journeys, q.oneChange(origin, dest, direct, limit)...)
		journeys = prune(journeys)
	}
	slices.SortStableFunc(journeys, func(a, b Journey) int {
		if c := a.Depart().Compare(b.Depart()); c != 0 {
			return c
		}
		if c := a.Arrive().Compare(b.Arrive()); c != 0 {
			return c
		}
		return cmp.Compare(len(a.Legs), len(b.Legs))
	})
	if len(journeys) > limit {
		journeys = journeys[:limit]
	}
	return journeys, fresh, nil
}

// LegOptions selects the boarding and alighting stations of a leg.
type LegOptions struct {
	// From and To default to the first and last call.
	From, To StationID
}

// Leg returns one trip on one service date with realtime applied.
func (s *Service) Leg(ctx context.Context, trip TripID, date Date, opt LegOptions) (*Leg, Freshness, error) {
	q := s.newQuery(time.Time{})
	fresh := s.freshness(q.snap, q.now)
	var leg Leg
	if t := q.st.ix.Trips[string(trip)]; t != nil {
		if date.IsZero() {
			date = DateOf(q.now, Brisbane)
		}
		leg = q.staticLeg(t, date, 0, int(t.Count)-1)
	} else if tu := q.snap.Trips[string(trip)]; tu != nil && tu.Rel == gtfsrt.RelAdded {
		l, ok := q.addedLeg(tu)
		if !ok {
			return nil, fresh, fmt.Errorf("trip %q: %w", trip, ErrNotFound)
		}
		leg = l
	} else {
		return nil, fresh, fmt.Errorf("trip %q: %w", trip, ErrNotFound)
	}
	if len(leg.Calls) == 0 {
		return nil, fresh, fmt.Errorf("trip %q has no stops: %w", trip, ErrNotFound)
	}

	leg.Board, leg.Alight = 0, len(leg.Calls)-1
	if opt.From != "" {
		if i := findCall(leg.Calls, opt.From, 0); i >= 0 {
			leg.Board = i
		}
	}
	if opt.To != "" {
		if i := findCall(leg.Calls, opt.To, leg.Board+1); i >= 0 {
			leg.Alight = i
		}
	}
	// Board and alight decide which stop alerts apply and the punctuality.
	var routeID string
	if t := q.st.ix.Trips[string(trip)]; t != nil {
		routeID = t.RouteID
	} else if tu := q.snap.Trips[string(trip)]; tu != nil {
		routeID = tu.RouteID
	}
	cancelled := leg.Punctuality == Cancelled
	leg.DelaySeconds = 0
	leg.Alerts, leg.Punctuality = q.alertsAndPunctuality(&leg, routeID, cancelled)
	return &leg, fresh, nil
}

func findCall(calls []Call, station StationID, from int) int {
	for i := from; i < len(calls); i++ {
		if calls[i].Station == station {
			return i
		}
	}
	return -1
}

// direct returns every boardable direct leg from origin to dest, sorted by
// expected departure. Results are memoised for the query.
func (q *query) direct(origin, dest StationID) []Leg {
	key := [2]StationID{origin, dest}
	if legs, ok := q.legs[key]; ok {
		return legs
	}
	ix := q.st.ix
	trips := intersectSorted(ix.Calling[string(origin)], ix.Calling[string(dest)])
	today := DateOf(q.at, Brisbane)
	from, to := q.at.Add(-3*time.Hour), q.at.Add(searchHorizon)

	var legs []Leg
	for _, off := range []int{-1, 0, 1} {
		date := today.AddDays(off)
		gd := date.GTFS()
		midnight := date.Midnight(Brisbane)
		for _, id := range trips {
			t := ix.Trips[id]
			if t == nil || !ix.ServiceActive(t.ServiceID, gd) {
				continue
			}
			sts := ix.TripStopTimes(t)
			b, a := boardAlight(ix, sts, origin, dest)
			if b < 0 {
				continue
			}
			dep := midnight.Add(secs(sts[b].Departure))
			if dep.Before(from) || dep.After(to) {
				continue
			}
			leg := q.staticLeg(t, date, b, a)
			if q.boardable(&leg) {
				legs = append(legs, leg)
			}
		}
	}

	// ADDED trips have no static counterpart and exist only in realtime.
	for _, tu := range q.snap.Trips {
		if tu.Rel != gtfsrt.RelAdded || ix.Trips[tu.TripID] != nil {
			continue
		}
		leg, ok := q.addedLeg(tu)
		if !ok {
			continue
		}
		b := findCall(leg.Calls, origin, 0)
		if b < 0 {
			continue
		}
		a := findCall(leg.Calls, dest, b+1)
		if a < 0 {
			continue
		}
		leg.Board, leg.Alight = b, a
		leg.DelaySeconds = 0
		leg.Alerts, leg.Punctuality = q.alertsAndPunctuality(&leg, tu.RouteID, false)
		if q.boardable(&leg) {
			legs = append(legs, leg)
		}
	}

	slices.SortStableFunc(legs, func(a, b Leg) int {
		if c := a.Depart().Compare(b.Depart()); c != 0 {
			return c
		}
		return a.Arrive().Compare(b.Arrive())
	})
	q.legs[key] = legs
	return legs
}

// boardAlight finds the first call at origin where pickup is allowed and the
// first later call at dest where drop-off is allowed.
func boardAlight(ix *gtfs.Index, sts []gtfs.StopTime, origin, dest StationID) (int, int) {
	stationOf := func(stopID string) StationID {
		if st, ok := ix.StationOf(stopID); ok {
			return StationID(st.ID)
		}
		return ""
	}
	for b, st := range sts {
		if stationOf(st.StopID) != origin || st.PickupType == 1 {
			continue
		}
		for a := b + 1; a < len(sts); a++ {
			if stationOf(sts[a].StopID) == dest && sts[a].DropOffType != 1 {
				return b, a
			}
		}
		return -1, -1
	}
	return -1, -1
}

// boardable reports whether the leg can still be caught at or after q.at.
func (q *query) boardable(l *Leg) bool {
	b := &l.Calls[l.Board]
	if b.Position == Passed {
		return false
	}
	if q.at.After(q.now) {
		return !b.Expected.Before(q.at)
	}
	// A train standing at the platform right now can still be caught even if
	// its predicted departure has just ticked past.
	return b.Position == Current || !b.Expected.Before(q.at.Add(-time.Minute))
}

// alightable reports whether the leg actually gets you to the destination.
func alightable(l *Leg) bool { return !l.Calls[l.Alight].Skipped }

// usable reports whether a leg can be ridden at all.
func usable(l *Leg) bool {
	return l.Punctuality != Cancelled && l.Punctuality != Skipping && alightable(l)
}

// oneChange pairs origin→X with X→dest legs across the interchanges.
func (q *query) oneChange(origin, dest StationID, direct []Leg, limit int) []Journey {
	// Only journeys departing before the limit-th direct train can make the
	// list; with too few direct trains, search the whole horizon.
	windowEnd := q.at.Add(searchHorizon)
	var usableDirect []Leg
	for _, l := range direct {
		if usable(&l) {
			usableDirect = append(usableDirect, l)
		}
	}
	if len(usableDirect) >= limit {
		windowEnd = usableDirect[limit-1].Depart()
	}

	best := map[string]Journey{} // per first-leg run: the earliest arrival
	for _, x := range q.st.interchanges {
		if x == origin || x == dest {
			continue
		}
		firsts := q.direct(origin, x)
		if len(firsts) == 0 {
			continue
		}
		seconds := q.direct(x, dest)
		if len(seconds) == 0 {
			continue
		}
		name := q.st.stations[q.st.byID[x]].Name
		for i := range firsts {
			l1 := firsts[i]
			if l1.Depart().After(windowEnd) {
				break
			}
			if !usable(&l1) {
				continue
			}
			// Pairing uses expected times, so a connection eroded below the
			// minimum by a late first train is never offered (SPEC.md §3.6).
			earliest := l1.Arrive().Add(MinConnection)
			k := sort.Search(len(seconds), func(k int) bool { return !seconds[k].Depart().Before(earliest) })
			for ; k < len(seconds); k++ {
				l2 := seconds[k]
				if l2.TripID == l1.TripID || !usable(&l2) {
					continue
				}
				buffer := l2.Depart().Sub(l1.Arrive())
				j := newJourney([]Leg{l1, l2}, &Change{
					Station: x, Name: name, Buffer: buffer, Tight: buffer < TightConnection,
				})
				key := string(l1.TripID) + "|" + l1.ServiceDate.String()
				if cur, ok := best[key]; !ok || better(j, cur) {
					best[key] = j
				}
				break
			}
		}
	}
	out := make([]Journey, 0, len(best))
	for _, j := range best {
		out = append(out, j)
	}
	return out
}

// better prefers the earlier arrival, then the more comfortable change.
func better(a, b Journey) bool {
	if c := a.Arrive().Compare(b.Arrive()); c != 0 {
		return c < 0
	}
	return a.Change.Buffer > b.Change.Buffer
}

// prune drops one-change journeys that a direct train matches or beats
// (leaving no earlier and arriving no later — direct wins ties), and
// one-change journeys dominated by another one-change journey.
func prune(js []Journey) []Journey {
	dominates := func(a, b *Journey) bool {
		return !a.Depart().Before(b.Depart()) && !a.Arrive().After(b.Arrive())
	}
	var out []Journey
	for i := range js {
		j := &js[i]
		if j.Change == nil {
			out = append(out, *j)
			continue
		}
		keep := true
		for k := range js {
			o := &js[k]
			if k == i || !usable(&o.Legs[0]) || (len(o.Legs) > 1 && !usable(&o.Legs[1])) {
				continue
			}
			if !dominates(o, j) {
				continue
			}
			if o.Change == nil {
				keep = false // a direct train is at least as good
				break
			}
			strictly := o.Depart().After(j.Depart()) || o.Arrive().Before(j.Arrive())
			if strictly || (k < i && o.Change.Buffer >= j.Change.Buffer) || o.Change.Buffer > j.Change.Buffer {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, *j)
		}
	}
	return out
}

func newJourney(legs []Leg, change *Change) Journey {
	p := legs[0].Punctuality
	for _, l := range legs[1:] {
		if l.Punctuality.severity() > p.severity() {
			p = l.Punctuality
		}
	}
	return Journey{Legs: legs, Change: change, Punctuality: p}
}

// intersectSorted returns the elements common to two sorted string slices.
func intersectSorted(a, b []string) []string {
	var out []string
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch c := cmp.Compare(a[i], b[j]); {
		case c < 0:
			i++
		case c > 0:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}
