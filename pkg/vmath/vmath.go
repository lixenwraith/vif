// Package vmath provides fixed-point (Q32.32) and float64 primitives for
// grid-based simulation: arithmetic, LUT trigonometry, 2D/3D vectors,
// ellipses, arcs, grid traversal and a fast seeded RNG.
//
// Fixed-point values are int64 with 32 fractional bits; Scale is 1.0.
// Grid coordinates are plain int cell indices. Point.Center is the only
// sanctioned cell -> precise-position conversion; it applies the half-cell
// offset that keeps physics and rendering aligned.
//
// The package depends only on the standard library.
package vmath

import "math"

// === Q32.32 representation ===

const (
	Shift int64 = 32
	Scale int64 = 1 << Shift
	Mask  int64 = Scale - 1
	Half  int64 = 1 << (Shift - 1)

	// ScaleF is Scale as float64 for conversion helpers
	ScaleF = float64(Scale)

	// CellCenter is the offset from a cell origin to its center (0.5)
	CellCenter int64 = Half
)

// === Lookup tables ===

const (
	// LUTSize is the resolution of the sin/cos/atan tables
	LUTSize = 1024
	LUTMask = LUTSize - 1

	// ExpLUTSize is the resolution of the exponential decay table
	ExpLUTSize = 256
	// ExpLUTMaxInput is the highest input mapped into the table, output saturates beyond it
	ExpLUTMaxInput = 512
	// ExpLUTDecayK controls curve steepness (higher = slower decay)
	ExpLUTDecayK = 30.0
)

// === Geometry ===

const (
	// TwoPi is a full rotation in radians (float path)
	TwoPi = 2.0 * math.Pi

	// TerminalAspect is a terminal cell's width:height ratio (1:2)
	TerminalAspect    int64 = Scale / 2
	TerminalAspectInv int64 = Scale * 2

	// EllipseSampleCount is the number of points sampled for arc availability
	EllipseSampleCount = 64
)

// === Grid types ===

// Point is an integer grid cell coordinate
type Point struct {
	X, Y int
}

// Center returns the Q32.32 position of the cell's center
func (p Point) Center() (px, py int64) {
	return FromInt(p.X) + CellCenter, FromInt(p.Y) + CellCenter
}

// Add returns the component-wise sum
func (p Point) Add(q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// Sub returns the component-wise difference
func (p Point) Sub(q Point) Point { return Point{X: p.X - q.X, Y: p.Y - q.Y} }

// PointAt returns the cell containing the Q32.32 position
func PointAt(px, py int64) Point { return Point{X: ToInt(px), Y: ToInt(py)} }

// Area is a rectangular grid region anchored at its top-left cell
type Area struct {
	X, Y          int // Top-left corner
	Width, Height int // Dimensions (minimum 1x1)
}

// Center returns the center cell of the area
func (a Area) Center() Point {
	return Point{X: a.X + a.Width/2, Y: a.Y + a.Height/2}
}

// Contains reports whether the cell lies inside the area
func (a Area) Contains(x, y int) bool {
	return x >= a.X && x < a.X+a.Width && y >= a.Y && y < a.Y+a.Height
}

// ContainsPoint reports whether the point lies inside the area
func (a Area) ContainsPoint(p Point) bool { return a.Contains(p.X, p.Y) }

// RandomPoint returns a random cell within the area
// Single-cell axes deliberately do not consume RNG state
func (a Area) RandomPoint(rng *FastRand) Point {
	p := Point{X: a.X, Y: a.Y}
	if a.Width > 1 {
		p.X += rng.Intn(a.Width)
	}
	if a.Height > 1 {
		p.Y += rng.Intn(a.Height)
	}
	return p
}

// DistributePoint returns the cell at index in row-major order
// Falls back to a random cell when index is out of range
func (a Area) DistributePoint(index int, rng *FastRand) Point {
	capacity := a.Width * a.Height
	if capacity <= 1 || index < 0 || index >= capacity {
		return a.RandomPoint(rng)
	}
	return Point{X: a.X + index%a.Width, Y: a.Y + index/a.Width}
}
