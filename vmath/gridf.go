package vmath

import "math"

// CenteredFromGridF converts integer grid coordinates to centered float64 position
func CenteredFromGridF(x, y int) (float64, float64) {
	return float64(x) + 0.5, float64(y) + 0.5
}

// GridTraverserF implements a zero-allocation iterator for Supercover DDA grid traversal
type GridTraverserF struct {
	currX, currY     int
	targetX, targetY int
	stepX, stepY     int

	tMaxX, tMaxY     float64
	tDeltaX, tDeltaY float64

	started bool
	done    bool
}

// NewGridTraverserF creates a new iterator from (x1, y1) to (x2, y2) in float64 space
func NewGridTraverserF(x1, y1, x2, y2 float64) GridTraverserF {
	ix, iy := int(math.Floor(x1)), int(math.Floor(y1))
	targetX, targetY := int(math.Floor(x2)), int(math.Floor(y2))

	t := GridTraverserF{
		currX: ix, currY: iy,
		targetX: targetX, targetY: targetY,
	}

	dx := x2 - x1
	dy := y2 - y1

	t.stepX, t.stepY = 1, 1
	if dx < 0 {
		t.stepX = -1
	}
	if dy < 0 {
		t.stepY = -1
	}

	if dx == 0 {
		t.tMaxX = math.MaxFloat64
		t.tDeltaX = 0
	} else {
		t.tDeltaX = math.Abs(1.0 / dx)
		if t.stepX > 0 {
			t.tMaxX = (math.Floor(x1) + 1.0 - x1) * t.tDeltaX
		} else {
			t.tMaxX = (x1 - math.Floor(x1)) * t.tDeltaX
		}
	}

	if dy == 0 {
		t.tMaxY = math.MaxFloat64
		t.tDeltaY = 0
	} else {
		t.tDeltaY = math.Abs(1.0 / dy)
		if t.stepY > 0 {
			t.tMaxY = (math.Floor(y1) + 1.0 - y1) * t.tDeltaY
		} else {
			t.tMaxY = (y1 - math.Floor(y1)) * t.tDeltaY
		}
	}

	return t
}

func (t *GridTraverserF) Next() bool {
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

func (t *GridTraverserF) Pos() (int, int) {
	return t.currX, t.currY
}

// CalculateCentroidF computes the exact geometric center of float points
func CalculateCentroidF(coords []float64) (float64, float64) {
	if len(coords) == 0 || len(coords)%2 != 0 {
		return 0, 0
	}
	var sumX, sumY float64
	count := float64(len(coords) / 2)

	for i := 0; i < len(coords); i += 2 {
		sumX += coords[i]
		sumY += coords[i+1]
	}

	return sumX / count, sumY / count
}
