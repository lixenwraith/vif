// Package vmath provides float64 primitives for grid-based simulation, on the
// standard library only: named math constants, scalar math, LUT trigonometry,
// 2D/3D vectors, ellipses, arcs, grid traversal, cell rays and seeded randomness.
// Point and Area hold integer cell indices; Point.Center is the sanctioned
// cell-to-precise conversion, adding the half-cell offset physics and rendering share.
package vmath

import "math"

// CellCenter is the offset from a cell origin to its center.
const CellCenter = 0.5

// RoundingMargin is an absolute distance from a threshold past which a value up to
// about 1e4 compares the same however a few float64 operations rounded it, so a
// cheaper form of a test can decide outside it and the exact expression within it.
const RoundingMargin = 1e-9

// DiagonalNum over DiagonalDen scales a radius to its diagonal step in integer
// cells: 7/10 for 1/√2. A float would round a cell apart past radius 16, and the
// simulation reproduces the integer form.
const DiagonalNum, DiagonalDen = 7, 10

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
	// TwoPi is a full rotation in radians
	TwoPi = 2.0 * math.Pi

	// EllipseSampleCount is the number of points sampled for arc availability
	EllipseSampleCount = 64
)

// === Grid types ===

// Point is an integer grid cell coordinate on a flat map: a column and a row.
type Point struct {
	X, Y int
}

// Center returns the float64 position of the cell's center
func (p Point) Center() (px, py float64) {
	return float64(p.X) + CellCenter, float64(p.Y) + CellCenter
}

// Add returns the component-wise sum
func (p Point) Add(q Point) Point { return Point{X: p.X + q.X, Y: p.Y + q.Y} }

// Sub returns the component-wise difference
func (p Point) Sub(q Point) Point { return Point{X: p.X - q.X, Y: p.Y - q.Y} }

// Layer is the cell in layer z of a volume
func (p Point) Layer(z int) Point3 { return Point3{X: p.X, Y: p.Y, Z: z} }

// Point3 is an integer cell coordinate in a volume: a column, a row and a layer.
type Point3 struct {
	X, Y, Z int
}

// Add returns the component-wise sum
func (p Point3) Add(q Point3) Point3 { return Point3{X: p.X + q.X, Y: p.Y + q.Y, Z: p.Z + q.Z} }

// Sub returns the component-wise difference
func (p Point3) Sub(q Point3) Point3 { return Point3{X: p.X - q.X, Y: p.Y - q.Y, Z: p.Z - q.Z} }

// PointAt returns the cell containing the float64 position
// Floor preserves cell mapping for negative coordinates.
func PointAt(px, py float64) Point {
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
