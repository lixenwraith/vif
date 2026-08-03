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

// LerpF performs linear interpolation between a and b
// t is in [0.0, 1.0] where 0.0 returns a, 1.0 returns b
func LerpF(a, b, t float64) float64 {
	return a + (b-a)*t
}

// ClampF restricts a value v between lo and hi
func ClampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// AbsF returns the absolute value
func AbsF(x float64) float64 {
	return math.Abs(x)
}

// SignF returns -1.0, 0.0, or 1.0
func SignF(x float64) float64 {
	if x < 0 {
		return -1.0
	}
	if x > 0 {
		return 1.0
	}
	return 0.0
}
