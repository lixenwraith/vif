package vmath

import "math"

// CenteredFromGrid converts integer grid coordinates to centered float64 position
func CenteredFromGrid(x, y int) (float64, float64) {
	return (Point{X: x, Y: y}).Center()
}

// GridFromCentered converts centered float64 position to integer grid coordinates
func GridFromCentered(px, py float64) (int, int) {
	p := PointAt(px, py)
	return p.X, p.Y
}

// GridTraverser implements a zero-allocation iterator for Supercover DDA grid traversal
type GridTraverser struct {
	currX, currY     int
	targetX, targetY int
	stepX, stepY     int

	tMaxX, tMaxY     float64
	tDeltaX, tDeltaY float64

	started bool
	done    bool
}

// NewGridTraverser creates a new iterator from (x1, y1) to (x2, y2) in float64 space
func NewGridTraverser(x1, y1, x2, y2 float64) GridTraverser {
	start := PointAt(x1, y1)
	target := PointAt(x2, y2)

	t := GridTraverser{
		currX: start.X, currY: start.Y,
		targetX: target.X, targetY: target.Y,
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

func (t *GridTraverser) Pos() (int, int) {
	return t.currX, t.currY
}

// CalculateCentroid returns the mean of interleaved X,Y coords, or (0,0) for an
// empty or odd-length slice
func CalculateCentroid(coords []float64) (float64, float64) {
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

// Ray is a straight strip of cells from (X, Y) toward (DX, DY), in any direction:
// steps 1 to Length along its major axis, each spanning Half(step) cells either
// side of the line across the minor axis — Near up to step Knee, Far beyond it. A
// ray aimed at a cell whose major distance is Knee passes through that cell.
type Ray struct {
	X, Y         int
	DX, DY       float64
	Length, Knee int
	Near, Far    int
}

func (r Ray) xMajor() bool { return Abs(r.DX) >= Abs(r.DY) }

// minorAt is the line's minor-axis offset at step i
func (r Ray) minorAt(i int) int {
	if r.DX == 0 && r.DY == 0 {
		return 0
	}
	if r.xMajor() {
		return int(math.Round(float64(i) * r.DY / Abs(r.DX)))
	}
	return int(math.Round(float64(i) * r.DX / Abs(r.DY)))
}

// Center returns the line's cell at step i
func (r Ray) Center(i int) (int, int) {
	if r.xMajor() {
		return r.X + i*int(Sign(r.DX)), r.Y + r.minorAt(i)
	}
	return r.X + r.minorAt(i), r.Y + i*int(Sign(r.DY))
}

// Half is the cells either side of the line at step i
func (r Ray) Half(i int) int {
	if i <= r.Knee {
		return r.Near
	}
	return r.Far
}

// Cell returns the cell `across` cells to the side of the line at step i
func (r Ray) Cell(i, across int) (int, int) {
	x, y := r.Center(i)
	if r.xMajor() {
		return x, y + across
	}
	return x + across, y
}

// Contains reports whether a cell lies inside the ray
func (r Ray) Contains(x, y int) bool {
	if r.DX == 0 && r.DY == 0 {
		return false
	}
	i, off := (x-r.X)*int(Sign(r.DX)), y-r.Y
	if !r.xMajor() {
		i, off = (y-r.Y)*int(Sign(r.DY)), x-r.X
	}
	return i >= 1 && i <= r.Length && IntAbs(off-r.minorAt(i)) <= r.Half(i)
}

// Octants are the eight grid directions clockwise from east, y growing down
var Octants = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
