package vmath

import "math"

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

func (r Ray) xMajor() bool { return AbsF(r.DX) >= AbsF(r.DY) }

// minorAt is the line's minor-axis offset at step i
func (r Ray) minorAt(i int) int {
	if r.DX == 0 && r.DY == 0 {
		return 0
	}
	if r.xMajor() {
		return int(math.Round(float64(i) * r.DY / AbsF(r.DX)))
	}
	return int(math.Round(float64(i) * r.DX / AbsF(r.DY)))
}

// Center returns the line's cell at step i
func (r Ray) Center(i int) (int, int) {
	if r.xMajor() {
		return r.X + i*int(SignF(r.DX)), r.Y + r.minorAt(i)
	}
	return r.X + r.minorAt(i), r.Y + i*int(SignF(r.DY))
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
	i, off := (x-r.X)*int(SignF(r.DX)), y-r.Y
	if !r.xMajor() {
		i, off = (y-r.Y)*int(SignF(r.DY)), x-r.X
	}
	return i >= 1 && i <= r.Length && IntAbs(off-r.minorAt(i)) <= r.Half(i)
}

// Octants are the eight grid directions clockwise from east, y growing down
var Octants = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
