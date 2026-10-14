package vmath

import "math"

// DegToRad converts degrees to radians
func DegToRad(deg float64) float64 {
	return deg * math.Pi / 180.0
}

// RadToDeg converts radians to degrees
func RadToDeg(rad float64) float64 {
	return rad * 180.0 / math.Pi
}

// Lerp performs linear interpolation between a and b
// t is in [0.0, 1.0] where 0.0 returns a, 1.0 returns b
func Lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

// Clamp restricts a value v between lo and hi
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Abs returns the absolute value
func Abs(x float64) float64 {
	return math.Abs(x)
}

// Sign returns -1.0, 0.0, or 1.0
func Sign(x float64) float64 {
	if x < 0 {
		return -1.0
	}
	if x > 0 {
		return 1.0
	}
	return 0.0
}
