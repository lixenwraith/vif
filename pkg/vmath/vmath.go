// Package vmath provides float64 primitives for grid-based simulation:
// scalar math, LUT trigonometry, vectors, ellipses, arcs, grid traversal,
// and deterministic randomness.
//
// Grid coordinates are plain integer cell indices. Point.CenterF is the
// sanctioned cell-to-precise-position conversion and applies the half-cell
// offset that keeps physics and rendering aligned.
//
// The package depends only on the standard library.
package vmath

import "math"

// CellCenterF is the offset from a cell origin to its center.
const CellCenterF = 0.5

// IntAbs returns the absolute value of a grid integer.
func IntAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

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

	// EllipseSampleCount is the number of points sampled for arc availability
	EllipseSampleCount = 64
)

// === Grid types ===

// Point is an integer grid cell coordinate
type Point struct {
	X, Y int
}

// CenterF returns the float64 position of the cell's center
func (p Point) CenterF() (px, py float64) {
	return float64(p.X) + CellCenterF, float64(p.Y) + CellCenterF
}

// Add returns the component-wise sum
func (p Point) Add(q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// Sub returns the component-wise difference
func (p Point) Sub(q Point) Point { return Point{X: p.X - q.X, Y: p.Y - q.Y} }

// PointAtF returns the cell containing the float64 position
// Floor preserves cell mapping for negative coordinates.
func PointAtF(px, py float64) Point {
	return Point{X: int(math.Floor(px)), Y: int(math.Floor(py))}
}

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
