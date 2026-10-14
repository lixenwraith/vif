package vmath

import "math"

// Normalize2D returns a unit vector
func Normalize2D(x, y float64) (float64, float64) {
	magSq := x*x + y*y
	if magSq == 0 {
		return 0, 0
	}
	inv := 1.0 / math.Sqrt(magSq)
	return x * inv, y * inv
}

// Magnitude returns vector length.
// math.Sqrt over math.Hypot: grid magnitudes are nowhere near float64
// overflow, and Hypot's rescale branches cost ~3.5x
func Magnitude(x, y float64) float64 {
	return math.Sqrt(x*x + y*y)
}

// MagnitudeSq returns squared magnitude without sqrt
func MagnitudeSq(x, y float64) float64 {
	return x*x + y*y
}

// ClampMagnitude limits vector to maxMag while preserving direction
func ClampMagnitude(x, y, maxMag float64) (float64, float64) {
	magSq := x*x + y*y
	if magSq <= maxMag*maxMag {
		return x, y
	}

	mag := math.Sqrt(magSq)
	if mag == 0 {
		return x, y
	}

	scale := maxMag / mag
	return x * scale, y * scale
}

// RotateVector rotates vector by angle in radians
func RotateVector(x, y, angleRad float64) (float64, float64) {
	cos := Cos(angleRad)
	sin := Sin(angleRad)
	return x*cos - y*sin, x*sin + y*cos
}

// ScaleVector multiplies vector by a scalar factor
func ScaleVector(x, y, factor float64) (float64, float64) {
	return x * factor, y * factor
}

// DotProduct returns x1*x2 + y1*y2
func DotProduct(x1, y1, x2, y2 float64) float64 {
	return x1*x2 + y1*y2
}

// Reflect returns velocity reflected off surface with given normal
func Reflect(velX, velY, normalX, normalY float64) (float64, float64) {
	dot := DotProduct(velX, velY, normalX, normalY)
	return velX - 2.0*dot*normalX, velY - 2.0*dot*normalY
}

// Perpendicular returns vector rotated 90° counter-clockwise
func Perpendicular(x, y float64) (float64, float64) {
	return -y, x
}

// ReflectAxisX returns velocity reflected off a vertical wall
func ReflectAxisX(velX, velY float64) (float64, float64) {
	return -velX, velY
}

// ReflectAxisY returns velocity reflected off a horizontal wall
func ReflectAxisY(velX, velY float64) (float64, float64) {
	return velX, -velY
}
