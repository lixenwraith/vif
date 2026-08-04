package vmath

// Ellipse utilities for shield, drain, and orbital calculations
// All operations use Q32.32 fixed-point with precomputed inverse squared radii

// EllipseDistSq returns normalized squared distance for ellipse containment
// Result <= Scale means point is inside ellipse
// invRxSq and invRySq are precomputed as Scale / (radius * radius)
// Evaluated in float64: the four-Mul fixed path benchmarks ~6.7x slower
func EllipseDistSq(dx, dy, invRxSq, invRySq int64) int64 {
	fdx, fdy := float64(dx), float64(dy)
	return int64((fdx*fdx*float64(invRxSq) + fdy*fdy*float64(invRySq)) * invScaleSq)
}

// EllipseContains returns true if point (dx, dy) is inside or on ellipse boundary
func EllipseContains(dx, dy, invRxSq, invRySq int64) bool {
	return EllipseDistSq(dx, dy, invRxSq, invRySq) <= Scale
}

// EllipseInvRadiiSq precomputes inverse squared radii for repeated ellipse checks
// Returns (invRxSq, invRySq) as Q32.32 values
func EllipseInvRadiiSq(rx, ry int64) (int64, int64) {
	rxSq := Mul(rx, rx)
	rySq := Mul(ry, ry)
	return Div(Scale, rxSq), Div(Scale, rySq)
}

// EllipseAlpha returns opacity for gradient rendering (0 at center, maxAlpha at edge)
// Uses squared distance directly—no sqrt needed for quadratic gradient
// Returns Q32.32 alpha value clamped to [0, maxAlpha]
func EllipseAlpha(dx, dy, invRxSq, invRySq, maxAlpha int64) int64 {
	distSq := EllipseDistSq(dx, dy, invRxSq, invRySq)
	if distSq > Scale {
		return 0 // Outside ellipse
	}
	return Mul(distSq, maxAlpha)
}

// ScaleToCircular transforms Y coordinate to make ellipse checks circular
// For terminal aspect 1:2, this doubles Y to normalize the space
func ScaleToCircular(dy int64) int64 {
	return dy << 1
}

// ScaleFromCircular reverses circular normalization for display
func ScaleFromCircular(dy int64) int64 {
	return dy >> 1
}

// CircleDistSq returns squared distance for circular containment
// Use after ScaleToCircular on Y to convert ellipse to circle check
// Evaluated in float64 for the same reason as EllipseDistSq
func CircleDistSq(dx, dy int64) int64 {
	fdx, fdy := float64(dx), float64(dy)
	return int64((fdx*fdx + fdy*fdy) * invScale)
}

// CircleContains returns true if point is inside circle of given radius
// radiusSq is precomputed as Mul(radius, radius)
func CircleContains(dx, dy, radiusSq int64) bool {
	return CircleDistSq(dx, dy) <= radiusSq
}

// EllipseContainsPoint checks if integer grid point (x,y) is inside ellipse centered at (cx,cy)
// invRxSq, invRySq are precomputed inverse squared radii from EllipseInvRadiiSq
// Ellipse equation: (dx²/rx² + dy²/ry²) <= 1  →  (dx² * invRxSq + dy² * invRySq) <= Scale
func EllipseContainsPoint(x, y, cx, cy int, invRxSq, invRySq int64) bool {
	dx := FromInt(x - cx)
	dy := FromInt(y - cy)
	return EllipseContains(dx, dy, invRxSq, invRySq)
}
