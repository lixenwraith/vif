package vmath

import "math"

// Normalize2DF returns a unit vector, zero-safe
func Normalize2DF(x, y float64) (float64, float64) {
	mag := math.Hypot(x, y)
	if mag == 0 {
		return 0, 0
	}
	return x / mag, y / mag
}

// MagnitudeF returns exact vector length using hypotenuse
func MagnitudeF(x, y float64) float64 {
	return math.Hypot(x, y)
}

// MagnitudeSqF returns squared magnitude without sqrt
func MagnitudeSqF(x, y float64) float64 {
	return x*x + y*y
}

// ClampMagnitudeF limits vector to maxMag while preserving direction
func ClampMagnitudeF(x, y, maxMag float64) (float64, float64) {
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

// RotateVectorF rotates vector by angle in radians
func RotateVectorF(x, y, angleRad float64) (float64, float64) {
	cos := math.Cos(angleRad)
	sin := math.Sin(angleRad)
	return x*cos - y*sin, x*sin + y*cos
}

// ScaleVectorF multiplies vector by a scalar factor
func ScaleVectorF(x, y, factor float64) (float64, float64) {
	return x * factor, y * factor
}

// DotProductF returns x1*x2 + y1*y2
func DotProductF(x1, y1, x2, y2 float64) float64 {
	return x1*x2 + y1*y2
}

// ReflectF returns velocity reflected off surface with given normal
func ReflectF(velX, velY, normalX, normalY float64) (float64, float64) {
	dot := DotProductF(velX, velY, normalX, normalY)
	return velX - 2.0*dot*normalX, velY - 2.0*dot*normalY
}

// PerpendicularF returns vector rotated 90° counter-clockwise
func PerpendicularF(x, y float64) (float64, float64) {
	return -y, x
}

// ReflectAxisXF returns velocity reflected off a vertical wall
func ReflectAxisXF(velX, velY float64) (float64, float64) {
	return -velX, velY
}

// ReflectAxisYF returns velocity reflected off a horizontal wall
func ReflectAxisYF(velX, velY float64) (float64, float64) {
	return velX, -velY
}
