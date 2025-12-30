// FILE: vmath/ellipse.go
// @lixen: #dev{feature[lightning(render)],feature[shield(render,system)],feature[spirit(render,system)]}
package vmath

// Ellipse utilities for shield, drain, and orbital calculations
// All operations use Q16.16 fixed-point with precomputed inverse squared radii

// EllipseDistSq returns normalized squared distance for ellipse containment
// Result <= Scale means point is inside ellipse
// invRxSq and invRySq are precomputed as Scale / (radius * radius)
func EllipseDistSq(dx, dy, invRxSq, invRySq int32) int32 {
	dxSq := Mul(dx, dx)
	dySq := Mul(dy, dy)
	return Mul(dxSq, invRxSq) + Mul(dySq, invRySq)
}

// EllipseContains returns true if point (dx, dy) is inside or on ellipse boundary
func EllipseContains(dx, dy, invRxSq, invRySq int32) bool {
	return EllipseDistSq(dx, dy, invRxSq, invRySq) <= Scale
}

// EllipseInvRadiiSq precomputes inverse squared radii for repeated ellipse checks
// Returns (invRxSq, invRySq) as Q16.16 values
func EllipseInvRadiiSq(rx, ry int32) (int32, int32) {
	rxSq := Mul(rx, rx)
	rySq := Mul(ry, ry)
	return Div(Scale, rxSq), Div(Scale, rySq)
}

// EllipseAlpha returns opacity for gradient rendering (0 at center, maxAlpha at edge)
// Uses squared distance directly—no sqrt needed for quadratic gradient
// Returns Q16.16 alpha value clamped to [0, maxAlpha]
func EllipseAlpha(dx, dy, invRxSq, invRySq, maxAlpha int32) int32 {
	distSq := EllipseDistSq(dx, dy, invRxSq, invRySq)
	if distSq > Scale {
		return 0 // Outside ellipse
	}
	return Mul(distSq, maxAlpha)
}

// AspectRatio constants for terminal character cells (width:height = 1:2)
const (
	TerminalAspect    int32 = Scale / 2 // 0.5 in Q16.16
	TerminalAspectInv int32 = Scale * 2 // 2.0 in Q16.16
)

// ScaleToCircular transforms Y coordinate to make ellipse checks circular
// For terminal aspect 1:2, this doubles Y to normalize the space
func ScaleToCircular(dy int32) int32 {
	return dy << 1
}

// ScaleFromCircular reverses circular normalization for display
func ScaleFromCircular(dy int32) int32 {
	return dy >> 1
}

// CircleDistSq returns squared distance for circular containment
// Use after ScaleToCircular on Y to convert ellipse to circle check
func CircleDistSq(dx, dy int32) int32 {
	return Mul(dx, dx) + Mul(dy, dy)
}

// CircleContains returns true if point is inside circle of given radius
// radiusSq is precomputed as Mul(radius, radius)
func CircleContains(dx, dy, radiusSq int32) bool {
	return CircleDistSq(dx, dy) <= radiusSq
}