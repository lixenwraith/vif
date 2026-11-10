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
	start, target := PointAt(x1, y1), PointAt(x2, y2)
	t := GridTraverser{currX: start.X, currY: start.Y, targetX: target.X, targetY: target.Y}
	t.stepX, t.tMaxX, t.tDeltaX = traverseAxis(x1, x2-x1)
	t.stepY, t.tMaxY, t.tDeltaY = traverseAxis(y1, y2-y1)
	return t
}

// traverseAxis is one axis of a traversal from a over d: its unit step, and the
// segment's parameter at its first cell boundary and from one boundary to the next
func traverseAxis(a, d float64) (step int, tMax, tDelta float64) {
	if d == 0 {
		return 1, math.MaxFloat64, 0
	}
	tDelta = math.Abs(1.0 / d)
	if d < 0 {
		return -1, (a - math.Floor(a)) * tDelta, tDelta
	}
	return 1, (math.Floor(a) + 1.0 - a) * tDelta, tDelta
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

// GridTraverser3 is GridTraverser through a volume: an axis short of the target
// steps unless another short of it crosses its boundary sooner, so a segment in
// one plane visits the cells GridTraverser does
type GridTraverser3 struct {
	curr, target, step Point3
	tMax, tDelta       Vec3
	started, done      bool
}

// NewGridTraverser3 creates a new iterator from a to b in float64 space
func NewGridTraverser3(a, b Vec3) GridTraverser3 {
	t := GridTraverser3{curr: PointAt3(a), target: PointAt3(b)}
	t.step.X, t.tMax.X, t.tDelta.X = traverseAxis(a.X, b.X-a.X)
	t.step.Y, t.tMax.Y, t.tDelta.Y = traverseAxis(a.Y, b.Y-a.Y)
	t.step.Z, t.tMax.Z, t.tDelta.Z = traverseAxis(a.Z, b.Z-a.Z)
	return t
}

func (t *GridTraverser3) Next() bool {
	if t.done {
		return false
	}
	if !t.started {
		t.started = true
		return true
	}
	if t.curr == t.target {
		t.done = true
		return false
	}
	// least is the soonest crossing among the axes short of the target
	x, y, z := t.curr.X != t.target.X, t.curr.Y != t.target.Y, t.curr.Z != t.target.Z
	least := math.Inf(1)
	if x {
		least = t.tMax.X
	}
	if y && t.tMax.Y < least {
		least = t.tMax.Y
	}
	if z && t.tMax.Z < least {
		least = t.tMax.Z
	}
	if x && !(least < t.tMax.X) {
		t.curr.X += t.step.X
		t.tMax.X += t.tDelta.X
	}
	if y && !(least < t.tMax.Y) {
		t.curr.Y += t.step.Y
		t.tMax.Y += t.tDelta.Y
	}
	if z && !(least < t.tMax.Z) {
		t.curr.Z += t.step.Z
		t.tMax.Z += t.tDelta.Z
	}
	return true
}

func (t *GridTraverser3) Pos() Point3 { return t.curr }

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
