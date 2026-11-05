package stream

import (
	"math"
	"testing"
)

func TestBackingScalesAndOrientation(t *testing.T) {
	for _, scale := range []float64{1, 2, 3} {
		tr := Transform{Width: int(400 * scale), Height: int(800 * scale), Scale: scale}
		p, err := tr.Backing(Point{100, 200}, 200, 400)
		if err != nil {
			t.Fatal(err)
		}
		if p.X != 200*scale || p.Y != 400*scale || tr.Logical(p) != (Point{200, 400}) {
			t.Fatal(scale, p, tr.Logical(p))
		}
	}
	for _, tc := range []struct {
		rotation int
		want     Point
	}{{0, Point{20, 40}}, {90, Point{159, 20}}, {180, Point{79, 159}}, {270, Point{40, 79}}} {
		tr := Transform{100, 200, 1, tc.rotation}
		p, err := tr.Backing(Point{20, 40}, 100, 200)
		if err != nil || p != tc.want {
			t.Fatal(tc, p, err)
		}
	}
	// Landscape screenshots already carry their oriented width/height.
	p, err := (Transform{800, 400, 2, 0}).Backing(Point{200, 100}, 400, 200)
	if err != nil || p != (Point{400, 200}) {
		t.Fatal(p, err)
	}
}
func TestLetterboxAndInvalidCoordinates(t *testing.T) {
	tr := Transform{100, 200, 1, 0}
	p, err := tr.Backing(Point{100, 100}, 200, 200)
	if err != nil || p != (Point{50, 100}) {
		t.Fatal(p, err)
	}
	for _, p := range []Point{{0, 100}, {150, 100}, {100, 200}, {-1, 0}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, err := tr.Backing(p, 200, 200); err == nil {
			t.Fatal("accepted", p)
		}
	}
	for _, tr := range []Transform{{0, 1, 1, 0}, {1, 1, 0, 0}, {1, 1, 1, 45}, {1, 1, math.NaN(), 0}} {
		if _, err := tr.Backing(Point{}, 1, 1); err == nil {
			t.Fatal("accepted invalid geometry", tr)
		}
	}
}
