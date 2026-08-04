package vmath

import (
	"math"
	"math/bits"
)

// --- Arithmetic ---

func FromInt(i int) int64       { return int64(i << Shift) }
func ToInt(f int64) int         { return int(f >> Shift) }
func FromFloat(f float64) int64 { return int64(f * ScaleF) }
func ToFloat(f int64) float64   { return float64(f) / ScaleF }

// Mul performs fixed point multiplication
// Valid while |a*b| < 2^96; truncates toward zero
func Mul(a, b int64) int64 {
	sign := int64(1)
	if a < 0 {
		a = -a
		sign = -1
	}
	if b < 0 {
		b = -b
		sign *= -1
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	// Q32.32 * Q32.32 = Q64.64; result is bits [32:95]
	res := int64((hi << 32) | (lo >> 32))

	if sign < 0 {
		return -res
	}
	return res
}

// Div uses hardware float division (~25x faster than 128-bit int div)
func Div(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return int64((float64(a) / float64(b)) * ScaleF)
}

// MulDiv computes (a * b) / c via float64 batching
func MulDiv(a, b, c int64) int64 {
	if c == 0 {
		return 0
	}
	return int64((float64(a) * float64(b)) / float64(c))
}

// Abs returns absolute value
func Abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Sign returns -Scale, 0, or Scale
func Sign(x int64) int64 {
	if x < 0 {
		return -Scale
	}
	if x > 0 {
		return Scale
	}
	return 0
}

// --- Trigonometry ---

// Sin returns sine of an angle where 0..Scale maps to 0..2pi
func Sin(angle int64) int64 {
	return SinLUT[(angle>>(Shift-10))&LUTMask]
}

func Cos(angle int64) int64 {
	return CosLUT[(angle>>(Shift-10))&LUTMask]
}

// --- Fast Approximations ---

// DistanceApprox uses alpha-max-beta-min (1, 0.375); peak error +6.8% at min/max = 0.375
func DistanceApprox(dx, dy int64) int64 {
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx < dy {
		dx, dy = dy, dx
	}
	// dist = max + 0.375*min
	return dx + (dy >> 2) + (dy >> 3)
}

// Sqrt returns the square root in Q32.32
// Evaluated in float64: hardware SQRT is ~300x faster than iterative integer
func Sqrt(x int64) int64 {
	if x <= 0 {
		return 0
	}
	// sqrt(x / 2^32) * 2^32 == sqrt(x) * 2^16
	return int64(math.Sqrt(float64(x)) * 65536.0)
}

// Lerp interpolates between a and b; t in [0, Scale]
func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}

// --- Misc ---

// IntAbs returns absolute value
func IntAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// --- Grid math ---

// CenteredFromGrid converts integer grid coordinates to centered Q32.32 position
func CenteredFromGrid(x, y int) (int64, int64) {
	return FromInt(x) + CellCenter, FromInt(y) + CellCenter
}

// GridFromCentered converts centered Q32.32 position to integer grid coordinates
func GridFromCentered(px, py int64) (int, int) {
	return ToInt(px), ToInt(py)
}
