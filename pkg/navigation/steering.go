package navigation

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// Source is the read side of a flow field: the direction to step from a cell and
// how far that cell is from the goal. Both FlowField and FlowFieldCache satisfy
// it, so a caller steering by a cached group field and one steering by a private
// route field share this file rather than each other's copy.
type Source interface {
	GetDirection(x, y int) int8
	GetDistance(x, y int) int
}

// UnitVectors are DirVectors as unit steering vectors, with Y halved before
// normalization for the terminal's 2:1 cell aspect: a cell is twice as tall as it
// is wide, so a diagonal that steps one cell in each axis is not a 45° heading.
var UnitVectors [8][2]float64

func init() {
	for i, vec := range DirVectors {
		fx := float64(vec[0])
		fy := float64(vec[1]) * 0.5
		if fx != 0 || fy != 0 {
			fx, fy = vmath.Normalize2DF(fx, fy)
		}
		UnitVectors[i] = [2]float64{fx, fy}
	}
}

// BestNeighborDirection is the escape hatch for a cell the field does not cover —
// one inside a wall, or one the goal cannot reach. It answers with the step toward
// whichever neighbour stands closest to the goal, and DirNone when no neighbour
// has been visited at all.
func BestNeighborDirection(src Source, x, y int) int8 {
	bestDir := DirNone
	bestDist := CostUnreachable

	for d := range DirCount {
		nx := x + DirVectors[d][0]
		ny := y + DirVectors[d][1]
		dist := src.GetDistance(nx, ny)
		if dist >= 0 && dist < bestDist {
			bestDist = dist
			bestDir = d
		}
	}
	return bestDir
}

// FlowVector is one cell's steering vector, and whether the field covers it.
func FlowVector(src Source, x, y int) (float64, float64, bool) {
	dir := src.GetDirection(x, y)
	if dir < 0 || dir >= DirCount {
		return 0, 0, false
	}
	return UnitVectors[dir][0], UnitVectors[dir][1], true
}

// InterpolatedDirection is the steering vector at a sub-cell position, bilinear
// over the four cells around it so a point entity crossing a cell boundary turns
// smoothly instead of snapping between eight headings. Cells the field does not
// cover drop out of the average rather than pulling it toward zero, and a position
// with no covered neighbour returns the zero vector for the caller to interpret.
func InterpolatedDirection(src Source, preciseX, preciseY float64) (float64, float64) {
	sampleX := preciseX - vmath.CellCenterF
	sampleY := preciseY - vmath.CellCenterF

	cell := vmath.PointAtF(sampleX, sampleY)
	x0, y0 := cell.X, cell.Y

	u := sampleX - float64(x0)
	v := sampleY - float64(y0)

	invU := 1.0 - u
	invV := 1.0 - v

	w00 := invU * invV
	w10 := u * invV
	w01 := invU * v
	w11 := u * v

	v00x, v00y, valid00 := FlowVector(src, x0, y0)
	v10x, v10y, valid10 := FlowVector(src, x0+1, y0)
	v01x, v01y, valid01 := FlowVector(src, x0, y0+1)
	v11x, v11y, valid11 := FlowVector(src, x0+1, y0+1)

	var sumX, sumY, totalWeight float64

	if valid00 {
		sumX += v00x * w00
		sumY += v00y * w00
		totalWeight += w00
	}
	if valid10 {
		sumX += v10x * w10
		sumY += v10y * w10
		totalWeight += w10
	}
	if valid01 {
		sumX += v01x * w01
		sumY += v01y * w01
		totalWeight += w01
	}
	if valid11 {
		sumX += v11x * w11
		sumY += v11y * w11
		totalWeight += w11
	}

	if totalWeight == 0 {
		return 0, 0
	}

	resX := sumX / totalWeight
	resY := sumY / totalWeight

	if resX != 0 || resY != 0 {
		return vmath.Normalize2DF(resX, resY)
	}
	return 0, 0
}
