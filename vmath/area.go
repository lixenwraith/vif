package vmath

import "github.com/lixenwraith/vi-fighter/core"

// AreaCenter returns the center point of the area
func AreaCenter(a core.Area) core.Point {
	return core.Point{
		X: a.X + a.Width/2,
		Y: a.Y + a.Height/2,
	}
}

// AreaContains checks if point is within area
func AreaContains(a core.Area, x, y int) bool {
	return x >= a.X && x < a.X+a.Width && y >= a.Y && y < a.Y+a.Height
}

// AreaRandomPoint returns a random point within area using provided RNG
func AreaRandomPoint(a core.Area, rng *FastRand) core.Point {
	x := a.X
	y := a.Y
	if a.Width > 1 {
		x += rng.Intn(a.Width)
	}
	if a.Height > 1 {
		y += rng.Intn(a.Height)
	}
	return core.Point{X: x, Y: y}
}

// AreaDistributePoint returns a point within area based on index for even distribution
// Falls back to random if index exceeds area capacity
func AreaDistributePoint(a core.Area, index int, rng *FastRand) core.Point {
	capacity := a.Width * a.Height
	if capacity <= 1 || index >= capacity {
		return AreaRandomPoint(a, rng)
	}
	return core.Point{
		X: a.X + (index % a.Width),
		Y: a.Y + (index / a.Width),
	}
}