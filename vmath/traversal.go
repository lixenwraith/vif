package vmath

import (
	"math"
)

// GridTraverser implements a zero-allocation iterator for Supercover DDA grid traversal.
// It replaces the callback-based Traverse function for high-performance hot paths.
type GridTraverser struct {
	currX, currY     int
	targetX, targetY int
	stepX, stepY     int

	tMaxX, tMaxY     int64
	tDeltaX, tDeltaY int64

	started bool
	done    bool
}

// NewGridTraverser creates a new iterator from (x1, y1) to (x2, y2).
// Coordinates are Q32.32 fixed-point.
func NewGridTraverser(x1, y1, x2, y2 int64) GridTraverser {
	ix, iy := ToInt(x1), ToInt(y1)
	targetX, targetY := ToInt(x2), ToInt(y2)

	t := GridTraverser{
		currX: ix, currY: iy,
		targetX: targetX, targetY: targetY,
	}

	dx := x2 - x1
	dy := y2 - y1

	t.stepX, t.stepY = 1, 1
	if dx < 0 {
		t.stepX = -1
		dx = -dx
	}
	if dy < 0 {
		t.stepY = -1
		dy = -dy
	}

	if dx == 0 {
		t.tMaxX = math.MaxInt64
	} else {
		t.tDeltaX = Div(Scale, dx)
		if t.stepX > 0 {
			t.tMaxX = Mul(Scale-(x1&Mask), t.tDeltaX)
		} else {
			t.tMaxX = Mul(x1&Mask, t.tDeltaX)
		}
	}

	if dy == 0 {
		t.tMaxY = math.MaxInt64
	} else {
		t.tDeltaY = Div(Scale, dy)
		if t.stepY > 0 {
			t.tMaxY = Mul(Scale-(y1&Mask), t.tDeltaY)
		} else {
			t.tMaxY = Mul((y1 & Mask), t.tDeltaY)
		}
	}

	return t
}

// Next advances the traverser to the next cell.
// Returns true if a valid cell is available via Pos().
func (t *GridTraverser) Next() bool {
	if t.done {
		return false
	}
	if !t.started {
		t.started = true
		return true
	}

	if t.currX == t.targetX && t.currY == t.targetY {
		t.done = true
		return false
	}

	if t.tMaxX < t.tMaxY {
		if t.currX != t.targetX {
			t.currX += t.stepX
			t.tMaxX += t.tDeltaX
		} else {
			t.currY += t.stepY
			t.tMaxY += t.tDeltaY
		}
	} else if t.tMaxX > t.tMaxY {
		if t.currY != t.targetY {
			t.currY += t.stepY
			t.tMaxY += t.tDeltaY
		} else {
			t.currX += t.stepX
			t.tMaxX += t.tDeltaX
		}
	} else {
		if t.currX != t.targetX {
			t.currX += t.stepX
			t.tMaxX += t.tDeltaX
		}
		if t.currY != t.targetY {
			t.currY += t.stepY
			t.tMaxY += t.tDeltaY
		}
	}

	return true
}

// Pos returns the current grid coordinates.
func (t *GridTraverser) Pos() (int, int) {
	return t.currX, t.currY
}

// --- 2D Traversal (Supercover DDA) ---

// Traverse visits every grid cell intersected by a line from (x1, y1) to (x2, y2), coordinates are Q32.32 fixed point
// Uses Supercover DDA to ensure no skipped cells, guaranteed to terminate by checking target bounds before stepping
func Traverse(x1, y1, x2, y2 int64, callback func(x, y int) bool) {
	ix, iy := ToInt(x1), ToInt(y1)
	targetX, targetY := ToInt(x2), ToInt(y2)

	if ix == targetX && iy == targetY {
		callback(ix, iy)
		return
	}

	dx := x2 - x1
	dy := y2 - y1

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
		dx = -dx
	}
	if dy < 0 {
		stepY = -1
		dy = -dy
	}

	// Calculate initial tMax and tDelta
	var tMaxX, tMaxY, tDeltaX, tDeltaY int64
	if dx == 0 {
		tMaxX = math.MaxInt64
	} else {
		tDeltaX = Div(Scale, dx)
		if stepX > 0 {
			tMaxX = Mul(Scale-(x1&Mask), tDeltaX)
		} else {
			tMaxX = Mul(x1&Mask, tDeltaX)
		}
	}

	if dy == 0 {
		tMaxY = math.MaxInt64
	} else {
		tDeltaY = Div(Scale, dy)
		if stepY > 0 {
			tMaxY = Mul(Scale-(y1&Mask), tDeltaY)
		} else {
			tMaxY = Mul((y1 & Mask), tDeltaY)
		}
	}

	if !callback(ix, iy) {
		return
	}

	// Loop until both indices match targets
	for ix != targetX || iy != targetY {
		if tMaxX < tMaxY {
			// Try stepping X
			if ix != targetX {
				ix += stepX
				tMaxX += tDeltaX
			} else {
				// X is done, forced to step Y
				iy += stepY
				tMaxY += tDeltaY
			}
		} else if tMaxX > tMaxY {
			// Try stepping Y
			if iy != targetY {
				iy += stepY
				tMaxY += tDeltaY
			} else {
				// Y is done, forced to step X
				ix += stepX
				tMaxX += tDeltaX
			}
		} else {
			// Diagonal step (tMaxX == tMaxY)
			if ix != targetX {
				ix += stepX
				tMaxX += tDeltaX
			}
			if iy != targetY {
				iy += stepY
				tMaxY += tDeltaY
			}
		}

		if !callback(ix, iy) {
			break
		}
	}
}

// CalculateCentroid computes the geometric center of a set of 2D points
// Returns (0,0) if the input slice is empty
// coords contains interleaved X,Y values (len must be even)
func CalculateCentroid(coords []int) (int, int) {
	if len(coords) == 0 || len(coords)%2 != 0 {
		return 0, 0
	}

	sumX, sumY := 0, 0
	count := len(coords) / 2

	for i := 0; i < len(coords); i += 2 {
		sumX += coords[i]
		sumY += coords[i+1]
	}

	return sumX / count, sumY / count
}

// Lerp performs linear interpolation between a and b
// t is in [0, Scale] where 0 returns a, Scale returns b
func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}