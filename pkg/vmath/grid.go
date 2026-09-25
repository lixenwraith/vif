package vmath

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

// Band is a straight strip of cells along one of eight directions: the cells 1 to
// Length steps from (X, Y) along (DX, DY), each widened Half cells across. The
// across axis is vertical for a horizontal band and horizontal otherwise, so a
// diagonal band is solid rather than a checkerboard.
type Band struct {
	X, Y, DX, DY, Length, Half int
}

// Contains reports whether a cell lies inside the band
func (b Band) Contains(x, y int) bool {
	if b.DY == 0 {
		along := (x - b.X) * b.DX
		return along >= 1 && along <= b.Length && IntAbs(y-b.Y) <= b.Half
	}
	along := (y - b.Y) * b.DY
	return along >= 1 && along <= b.Length && IntAbs(x-(b.X+along*b.DX)) <= b.Half
}

// Cell returns the cell `along` steps down the band and `across` cells to its side
func (b Band) Cell(along, across int) (int, int) {
	x, y := b.X+along*b.DX, b.Y+along*b.DY
	if b.DY == 0 {
		return x, y + across
	}
	return x + across, y
}

// Octants are the eight grid directions clockwise from east, y growing down
var Octants = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}

// Octant snaps a vector to the nearest of the eight grid directions; zero stays zero
func Octant(dx, dy float64) (int, int) {
	const tanEighth = 0.41421356237309503 // tan(π/8), the bisector of an axis and a diagonal
	ax, ay := AbsF(dx), AbsF(dy)
	sx, sy := int(SignF(dx)), int(SignF(dy))
	switch {
	case ay <= tanEighth*ax:
		return sx, 0
	case ax <= tanEighth*ay:
		return 0, sy
	}
	return sx, sy
}
