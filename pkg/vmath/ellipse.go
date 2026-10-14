package vmath

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

// CircleDistSq returns squared distance for circular containment
func CircleDistSq(dx, dy float64) float64 {
	return dx*dx + dy*dy
}

// CircleContains returns true if point is inside circle of given radius
func CircleContains(dx, dy, radiusSq float64) bool {
	return CircleDistSq(dx, dy) <= radiusSq
}

// ScaleToCircular transforms Y coordinate to make ellipse checks circular (1:2 terminal aspect)
func ScaleToCircular(dy float64) float64 {
	return dy * 2.0
}

// ScaleFromCircular reverses circular normalization for display
func ScaleFromCircular(dy float64) float64 {
	return dy * 0.5
}

// EllipseContainsPoint checks if grid point (x,y) is inside ellipse centered at (cx,cy)
func EllipseContainsPoint(x, y, cx, cy int, invRxSq, invRySq float64) bool {
	return EllipseContains(float64(x-cx), float64(y-cy), invRxSq, invRySq)
}
