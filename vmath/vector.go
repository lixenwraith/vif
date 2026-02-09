package vmath

import (
	"math"
)

// Normalize2D returns unit vector in Q32.32, zero-safe
func Normalize2D(x, y int64) (nx, ny int64) {
	// Avoid multiple Div calls, convert to float, math, convert back
	fx, fy := float64(x), float64(y)
	mag := math.Sqrt(fx*fx + fy*fy)
	if mag == 0 {
		return 0, 0
	}

	// Multiply by ScaleF/mag to get back to fixed point
	inv := ScaleF / mag
	return int64(fx * inv), int64(fy * inv)
}

// Magnitude returns exact vector length using hardware sqrt - true Euclidean distance sqrt(x² + y²)
// Optimization: Manually inline the square calculation to avoid Mul call overhead
func Magnitude(x, y int64) int64 {
	// Convert to float immediately to use hardware SQRT
	fx, fy := float64(x), float64(y)
	// sqrt(x² + y²) where x,y are Q32.32 yields Q32.32 directly
	return int64(math.Sqrt(fx*fx + fy*fy))
}

// MagnitudeSq returns squared magnitude without sqrt
func MagnitudeSq(x, y int64) int64 {
	return Mul(x, x) + Mul(y, y)
}

// ClampMagnitude limits vector to maxMag while preserving direction
func ClampMagnitude(x, y, maxMag int64) (cx, cy int64) {
	// 1. Check squared magnitude in float to avoid Sqrt if possible
	// (Float check is faster than int Mul+Mul check due to overflow handling in Mul)
	fx, fy := float64(x), float64(y)
	magSq := fx*fx + fy*fy
	fMax := float64(maxMag)

	if magSq <= fMax*fMax {
		return x, y
	}

	// 2. Slow path: Calculate scale factor
	mag := math.Sqrt(magSq)
	if mag == 0 {
		return x, y
	}

	// scale = maxMag / mag
	scale := fMax / mag
	return int64(fx * scale), int64(fy * scale)
}

// MagnitudeApprox returns approximate vector length (~4% error)
// Uses alpha-max-beta-min; faster than Sqrt and Magnitude for non-critical paths
func MagnitudeApprox(x, y int64) int64 {
	return DistanceApprox(x, y)
}

// ClampMagnitudeApprox limits vector using approximate magnitude (~4% error)
// Faster than ClampMagnitude for non-critical physics
func ClampMagnitudeApprox(x, y, maxMag int64) (cx, cy int64) {
	mag := MagnitudeApprox(x, y)
	if mag <= maxMag || mag == 0 {
		return x, y
	}
	scale := Div(maxMag, mag)
	return Mul(x, scale), Mul(y, scale)
}

// RotateVector rotates vector by angle using precomputed Sin/Cos LUT
// angle is in Q32.32 where Scale = 2π (full rotation)
func RotateVector(x, y, angle int64) (rx, ry int64) {
	cos := Cos(angle)
	sin := Sin(angle)
	rx = Mul(x, cos) - Mul(y, sin)
	ry = Mul(x, sin) + Mul(y, cos)
	return rx, ry
}

// ScaleVector multiplies vector by scalar factor
func ScaleVector(x, y, factor int64) (sx, sy int64) {
	return Mul(x, factor), Mul(y, factor)
}

// DotProduct returns x1*x2 + y1*y2 in Q32.32
func DotProduct(x1, y1, x2, y2 int64) int64 {
	return Mul(x1, x2) + Mul(y1, y2)
}

// Reflect returns velocity reflected off surface with given normal
// vel' = vel - 2 * dot(vel, normal) * normal
func Reflect(velX, velY, normalX, normalY int64) (rx, ry int64) {
	dot := DotProduct(velX, velY, normalX, normalY)
	dot2 := dot << 1 // 2 * dot
	rx = velX - Mul(dot2, normalX)
	ry = velY - Mul(dot2, normalY)
	return rx, ry
}

// Perpendicular returns vector rotated 90° counter-clockwise
func Perpendicular(x, y int64) (px, py int64) {
	return -y, x
}

// ReflectAxisX returns velocity reflected off a vertical wall (X axis boundary)
// Use for left/right screen edge collision
func ReflectAxisX(velX, velY int64) (int64, int64) {
	return -velX, velY
}

// ReflectAxisY returns velocity reflected off a horizontal wall (Y axis boundary)
// Use for top/bottom screen edge collision
func ReflectAxisY(velX, velY int64) (int64, int64) {
	return velX, -velY
}