package stream

import (
	"errors"
	"math"
)

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Transform maps an object-fit:contain viewport to the backing pixel space.
// Rotation is clockwise from encoded image space; Scale converts backing pixels
// to logical simulator points for the input backend.
type Transform struct {
	Width, Height int
	Scale         float64
	Rotation      int
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (t Transform) Valid() bool {
	return t.Width > 0 && t.Height > 0 && finite(t.Scale) && t.Scale > 0 && (t.Rotation == 0 || t.Rotation == 90 || t.Rotation == 180 || t.Rotation == 270)
}
func (t Transform) Backing(p Point, vw, vh float64) (Point, error) {
	if !t.Valid() || !finite(p.X) || !finite(p.Y) || !finite(vw) || !finite(vh) || vw <= 0 || vh <= 0 {
		return Point{}, errors.New("invalid viewport or geometry")
	}
	w, h := float64(t.Width), float64(t.Height)
	ratio := math.Min(vw/w, vh/h)
	x, y := (p.X-(vw-w*ratio)/2)/ratio, (p.Y-(vh-h*ratio)/2)/ratio
	if x < 0 || y < 0 || x >= w || y >= h {
		return Point{}, errors.New("point outside simulator image")
	}
	switch t.Rotation {
	case 90:
		x, y = h-y-1, x
	case 180:
		x, y = w-x-1, h-y-1
	case 270:
		x, y = y, w-x-1
	}
	return Point{math.Max(0, x), math.Max(0, y)}, nil
}
func (t Transform) Logical(p Point) Point { return Point{p.X / t.Scale, p.Y / t.Scale} }
