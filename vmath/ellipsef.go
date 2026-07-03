package vmath

// EllipseDistSqF returns normalized squared distance for ellipse containment
// Result <= 1.0 means point is inside ellipse
func EllipseDistSqF(dx, dy, invRxSq, invRySq float64) float64 {
	return dx*dx*invRxSq + dy*dy*invRySq
}

// EllipseContainsF returns true if point (dx, dy) is inside or on ellipse boundary
func EllipseContainsF(dx, dy, invRxSq, invRySq float64) bool {
	return EllipseDistSqF(dx, dy, invRxSq, invRySq) <= 1.0
}

// EllipseInvRadiiSqF precomputes inverse squared radii for repeated checks
func EllipseInvRadiiSqF(rx, ry float64) (float64, float64) {
	return 1.0 / (rx * rx), 1.0 / (ry * ry)
}

// EllipseAlphaF returns opacity for gradient rendering [0.0, maxAlpha]
func EllipseAlphaF(dx, dy, invRxSq, invRySq, maxAlpha float64) float64 {
	distSq := EllipseDistSqF(dx, dy, invRxSq, invRySq)
	if distSq > 1.0 {
		return 0.0 // Outside ellipse
	}
	return distSq * maxAlpha
}

// CircleDistSqF returns squared distance for circular containment
func CircleDistSqF(dx, dy float64) float64 {
	return dx*dx + dy*dy
}

// CircleContainsF returns true if point is inside circle of given radius
func CircleContainsF(dx, dy, radiusSq float64) bool {
	return CircleDistSqF(dx, dy) <= radiusSq
}
