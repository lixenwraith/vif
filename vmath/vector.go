package vmath

// @lixen: #dev{base(core),feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

// Normalize2D returns unit vector in Q16.16, zero-safe
// Uses DistanceApprox for performance (~4% error acceptable for game physics)
func Normalize2D(x, y int32) (nx, ny int32) {
	mag := DistanceApprox(x, y)
	if mag == 0 {
		return 0, 0
	}
	return Div(x, mag), Div(y, mag)
}

// Magnitude returns vector length using DistanceApprox
func Magnitude(x, y int32) int32 {
	return DistanceApprox(x, y)
}

// MagnitudeSq returns squared magnitude without sqrt
// Returns int64 to prevent overflow for large vectors
func MagnitudeSq(x, y int32) int64 {
	return int64(Mul(x, x)) + int64(Mul(y, y))
}

// ClampMagnitude limits vector to maxMag while preserving direction
// Returns unchanged vector if magnitude <= maxMag
func ClampMagnitude(x, y, maxMag int32) (cx, cy int32) {
	mag := Magnitude(x, y)
	if mag <= maxMag || mag == 0 {
		return x, y
	}
	scale := Div(maxMag, mag)
	return Mul(x, scale), Mul(y, scale)
}

// RotateVector rotates vector by angle using precomputed Sin/Cos LUT
// angle is in Q16.16 where Scale = 2π (full rotation)
func RotateVector(x, y, angle int32) (rx, ry int32) {
	cos := Cos(angle)
	sin := Sin(angle)
	rx = Mul(x, cos) - Mul(y, sin)
	ry = Mul(x, sin) + Mul(y, cos)
	return rx, ry
}

// ScaleVector multiplies vector by scalar factor
func ScaleVector(x, y, factor int32) (sx, sy int32) {
	return Mul(x, factor), Mul(y, factor)
}

// DotProduct returns x1*x2 + y1*y2 in Q16.16
func DotProduct(x1, y1, x2, y2 int32) int32 {
	return Mul(x1, x2) + Mul(y1, y2)
}

// Reflect returns velocity reflected off surface with given normal
// vel' = vel - 2 * dot(vel, normal) * normal
func Reflect(velX, velY, normalX, normalY int32) (rx, ry int32) {
	dot := DotProduct(velX, velY, normalX, normalY)
	dot2 := dot << 1 // 2 * dot
	rx = velX - Mul(dot2, normalX)
	ry = velY - Mul(dot2, normalY)
	return rx, ry
}

// Perpendicular returns vector rotated 90° counter-clockwise
func Perpendicular(x, y int32) (px, py int32) {
	return -y, x
}

// ReflectAxisX returns velocity reflected off a vertical wall (X axis boundary)
// Use for left/right screen edge collision
func ReflectAxisX(velX, velY int32) (int32, int32) {
	return -velX, velY
}

// ReflectAxisY returns velocity reflected off a horizontal wall (Y axis boundary)
// Use for top/bottom screen edge collision
func ReflectAxisY(velX, velY int32) (int32, int32) {
	return velX, -velY
}