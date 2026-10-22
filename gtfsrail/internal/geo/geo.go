// Package geo holds the small amount of plane geometry the rail index needs:
// polyline simplification for shapes. Projection for display is the UI's job.
package geo

import (
	"container/heap"
	"math"
)

// Point is a WGS84 coordinate in decimal degrees.
type Point struct{ Lat, Lon float64 }

// Simplify returns a Douglas–Peucker simplification with at most maxPts points,
// always keeping both endpoints. Input with <= maxPts points is returned unchanged (copied).
//
// Refinement is greedy: starting from the two endpoints it repeatedly inserts
// the point that deviates furthest from the current approximation, so the
// result is the best maxPts-point prefix of the Douglas–Peucker ordering. It may
// return fewer than maxPts points when the remaining points lie within about a
// centimetre of the approximation. A maxPts below 2 is treated as 2.
//
// Distances are measured on an equirectangular projection about the polyline's
// first point: within about 1% over a ≤100 km route, which only perturbs the
// choice between near-equal candidates.
func Simplify(pts []Point, maxPts int) []Point {
	maxPts = max(maxPts, 2)
	if len(pts) <= maxPts {
		return append([]Point(nil), pts...)
	}

	kx := math.Cos(pts[0].Lat * math.Pi / 180)
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	kept := 2

	h := &segHeap{}
	push := func(a, b int) {
		if s, ok := farthest(pts, a, b, kx); ok {
			heap.Push(h, s)
		}
	}
	push(0, len(pts)-1)
	for kept < maxPts && h.Len() > 0 {
		s := heap.Pop(h).(segment)
		keep[s.idx] = true
		kept++
		push(s.a, s.idx)
		push(s.idx, s.b)
	}

	out := make([]Point, 0, kept)
	for i, k := range keep {
		if k {
			out = append(out, pts[i])
		}
	}
	return out
}

// segment is an open span (a, b) of the input whose farthest interior point is idx.
type segment struct {
	a, b, idx int
	dist      float64
}

// onLine is the squared deviation, in degrees², below which a point counts as
// lying on the segment: (1e-7°)² ≈ 1 cm, far below any visible difference
// and above floating-point noise.
const onLine = 1e-14

// farthest finds the interior point of (a, b) furthest from segment a–b.
// It reports false when the span has no interior point or every interior
// point lies on the segment.
func farthest(pts []Point, a, b int, kx float64) (segment, bool) {
	best := segment{a: a, b: b, idx: -1, dist: onLine}
	ax, ay := pts[a].Lon*kx, pts[a].Lat
	bx, by := pts[b].Lon*kx, pts[b].Lat
	for i := a + 1; i < b; i++ {
		d := segDist(pts[i].Lon*kx, pts[i].Lat, ax, ay, bx, by)
		if d > best.dist {
			best.dist, best.idx = d, i
		}
	}
	return best, best.idx >= 0
}

// segDist returns the squared distance from p to segment a–b.
func segDist(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = ((px-ax)*dx + (py-ay)*dy) / l2
		t = math.Max(0, math.Min(1, t))
	}
	ex, ey := ax+t*dx-px, ay+t*dy-py
	return ex*ex + ey*ey
}

// segHeap is a max-heap of segments by deviation; ties break on index so the
// output is deterministic.
type segHeap []segment

func (h segHeap) Len() int { return len(h) }
func (h segHeap) Less(i, j int) bool {
	if h[i].dist != h[j].dist {
		return h[i].dist > h[j].dist
	}
	return h[i].idx < h[j].idx
}
func (h segHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *segHeap) Push(x any)   { *h = append(*h, x.(segment)) }
func (h *segHeap) Pop() any {
	old := *h
	s := old[len(old)-1]
	*h = old[:len(old)-1]
	return s
}
