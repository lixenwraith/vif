package vmath

import "math"

// EllipseDistSq returns normalized squared distance for ellipse containment
// Result <= 1.0 means point is inside ellipse
func EllipseDistSq(dx, dy, invRxSq, invRySq float64) float64 {
	return dx*dx*invRxSq + dy*dy*invRySq
}

// EllipseContains returns true if point (dx, dy) is inside or on ellipse boundary
func EllipseContains(dx, dy, invRxSq, invRySq float64) bool {
	return EllipseDistSq(dx, dy, invRxSq, invRySq) <= 1.0
}

// EllipseInvRadiiSq precomputes inverse squared radii for repeated checks
func EllipseInvRadiiSq(rx, ry float64) (float64, float64) {
	var invRxSq, invRySq float64
	if rx != 0 {
		invRxSq = 1.0 / (rx * rx)
	}
	if ry != 0 {
		invRySq = 1.0 / (ry * ry)
	}
	return invRxSq, invRySq
}

// EllipseAlpha returns opacity for gradient rendering [0.0, maxAlpha]
func EllipseAlpha(dx, dy, invRxSq, invRySq, maxAlpha float64) float64 {
	distSq := EllipseDistSq(dx, dy, invRxSq, invRySq)
	if distSq > 1.0 {
		return 0.0 // Outside ellipse
	}
	return distSq * maxAlpha
}

// EllipsoidInvRadiiSq precomputes inverse squared radii for repeated checks
func EllipsoidInvRadiiSq(rx, ry, rz float64) Vec3 {
	inv := Vec3{}
	inv.X, inv.Y = EllipseInvRadiiSq(rx, ry)
	if rz != 0 {
		inv.Z = 1.0 / (rz * rz)
	}
	return inv
}

// EllipsoidDistSq returns normalized squared distance for ellipsoid containment
// Result <= 1.0 means the offset d is inside the ellipsoid
func EllipsoidDistSq(d, invRadiiSq Vec3) float64 {
	return d.X*d.X*invRadiiSq.X + d.Y*d.Y*invRadiiSq.Y + d.Z*d.Z*invRadiiSq.Z
}

// EllipsoidContains returns true if offset d is inside or on the ellipsoid boundary
func EllipsoidContains(d, invRadiiSq Vec3) bool {
	return EllipsoidDistSq(d, invRadiiSq) <= 1.0
}

// EllipsoidContainsPoint checks if cell p is inside the ellipsoid centered at cell c
func EllipsoidContainsPoint(p, c Point3, invRadiiSq Vec3) bool {
	return EllipsoidContains(Vec3{float64(p.X - c.X), float64(p.Y - c.Y), float64(p.Z - c.Z)}, invRadiiSq)
}

// CircleDistSq returns squared distance for circular containment
func CircleDistSq(dx, dy float64) float64 {
	return dx*dx + dy*dy
}

// CircleContains returns true if point is inside circle of given radius
func CircleContains(dx, dy, radiusSq float64) bool {
	return CircleDistSq(dx, dy) <= radiusSq
}

// Aspect is a cell's width over its height. A terminal cell is about twice as tall
// as it is wide, and the game's geometry is authored at that reference, RefAspect;
// a host whose cells differ reports its own, and every vertical extent follows.
type Aspect float64

const RefAspect Aspect = 0.5

// Y is a vertical extent authored at RefAspect, in cells at this aspect
func (a Aspect) Y(y float64) float64 { return y * float64(a) / float64(RefAspect) }

// YInt is Y for a whole number of cells, rounded
func (a Aspect) YInt(y int) int { return int(math.Round(a.Y(float64(y)))) }

// Circular is a vertical cell distance in the units of a horizontal one, so a
// circle test reads it as circular
func (a Aspect) Circular(dy float64) float64 { return dy / float64(a) }

// FromCircular is Circular's inverse: a vertical extent in cells
func (a Aspect) FromCircular(dy float64) float64 { return dy * float64(a) }

// EllipseContainsPoint checks if grid point (x,y) is inside ellipse centered at (cx,cy)
func EllipseContainsPoint(x, y, cx, cy int, invRxSq, invRySq float64) bool {
	return EllipseContains(float64(x-cx), float64(y-cy), invRxSq, invRySq)
}
