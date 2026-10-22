package geo

import (
	"math"
	"slices"
	"testing"
)

// wiggle builds an n-point polyline that never lies on a straight line.
func wiggle(n int) []Point {
	pts := make([]Point, n)
	for i := range n {
		t := float64(i) / float64(n-1)
		pts[i] = Point{Lat: -27.4 - 0.3*t + 0.01*math.Sin(t*40), Lon: 153.0 + 0.2*t + 0.005*math.Cos(t*23)}
	}
	return pts
}

func TestSimplifyCapsPointsAndKeepsEndpoints(t *testing.T) {
	in := wiggle(603)
	out := Simplify(in, 120)
	if len(out) > 120 {
		t.Fatalf("len = %d, want <= 120", len(out))
	}
	if len(out) < 100 {
		t.Errorf("len = %d; a wiggly line should use most of the budget", len(out))
	}
	if out[0] != in[0] || out[len(out)-1] != in[len(in)-1] {
		t.Errorf("endpoints changed: got %v..%v, want %v..%v", out[0], out[len(out)-1], in[0], in[len(in)-1])
	}
	// Output is an order-preserving subsequence of the input.
	j := 0
	for _, p := range out {
		for j < len(in) && in[j] != p {
			j++
		}
		if j == len(in) {
			t.Fatalf("output point %v is not an in-order input point", p)
		}
		j++
	}
}

func TestSimplifyShortInputUnchangedAndCopied(t *testing.T) {
	for _, n := range []int{0, 1, 2, 120} {
		in := wiggle(max(n, 2))[:n]
		out := Simplify(in, 120)
		if !slices.Equal(in, out) {
			t.Fatalf("n=%d: output differs from input", n)
		}
		if n > 0 && &out[0] == &in[0] {
			t.Fatalf("n=%d: output aliases input", n)
		}
	}
}

func TestSimplifyCollinearCollapsesToEndpoints(t *testing.T) {
	in := make([]Point, 50)
	for i := range in {
		in[i] = Point{Lat: float64(i), Lon: float64(i)}
	}
	out := Simplify(in, 10)
	if want := []Point{in[0], in[49]}; !slices.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestSimplifyKeepsFarthestPoint(t *testing.T) {
	in := []Point{{0, 0}, {0, 1}, {5, 2}, {0, 3}, {0, 4}}
	out := Simplify(in, 3)
	if want := []Point{{0, 0}, {5, 2}, {0, 4}}; !slices.Equal(out, want) {
		t.Fatalf("got %v, want %v", out, want)
	}
}

func TestSimplifyClosedLoop(t *testing.T) {
	// Start and end coincide; the degenerate base segment must not lose the loop.
	in := []Point{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}}
	out := Simplify(in, 4)
	if len(out) != 4 || out[0] != in[0] || out[3] != in[4] {
		t.Fatalf("got %v", out)
	}
}
