package vmath

import (
	"math"
	"math/bits"
	"unsafe"
)

// TODO: try making these typed int64 and see how bad is the refactor
// Q32.32 Fixed Point constants
const (
	Shift       = 32
	Scale int64 = 1 << Shift
	Mask  int64 = Scale - 1
	Half  int64 = 1 << (Shift - 1)

	ScaleF = float64(Scale) // Helper for float ops

	// CellCenter is the fixed-point offset to the center of a grid cell (0.5 in Q32.32)
	CellCenter int64 = Half
)

const (
	LUTSize = 1024
	LUTMask = LUTSize - 1
)

// --- Arithmetic ---

func FromInt(i int) int64       { return int64(i << Shift) }
func ToInt(f int64) int         { return int(f >> Shift) }
func FromFloat(f float64) int64 { return int64(f * ScaleF) }
func ToFloat(f int64) float64   { return float64(f) / ScaleF }

// Mul performs fixed point multiplication
// Optimization: streamlined logic to encourage inlining
func Mul(a, b int64) int64 {
	// Fast path: check sign to use unsigned multiplication
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
	// Q32.32 * Q32.32 = Q64.64. Result is bits [32:95]
	// We want (hi << 32) | (lo >> 32)
	res := int64((hi << 32) | (lo >> 32))

	if sign < 0 {
		return -res
	}
	return res
}

// Div now uses hardware float division (~25x faster than 128-bit int div)
func Div(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	// Convert to float, divide, scale back
	return int64((float64(a) / float64(b)) * ScaleF)
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

// MulDiv computes (a * b) / c
// Optimization: Switched to float64 from 128-bit precision for performance (~100x speedup)
func MulDiv(a, b, c int64) int64 {
	if c == 0 {
		return 0
	}
	// Use float64 batching for speed
	return int64((float64(a) * float64(b)) / float64(c))
}

// Lerp performs linear interpolation between a and b
// t is in [0, Scale] where 0 returns a, Scale returns b
func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}

// --- Trigonometry ---

// Sin returns sine of an angle where angle 0..Scale maps to 0..2pi
func Sin(angle int64) int64 {
	return SinLUT[(angle>>(Shift-10))&LUTMask]
}

func Cos(angle int64) int64 {
	return CosLUT[(angle>>(Shift-10))&LUTMask]
}

// --- Fast Approximations ---

// InvSqrt implements the fast inverse square root (Quake III algorithm)
func InvSqrt(n float32) float32 {
	if n == 0 {
		return 0
	}
	n2 := n * 0.5
	y := n
	i := *(*int64)(unsafe.Pointer(&y))
	i = 0x5f3759df - (i >> 1)
	y = *(*float32)(unsafe.Pointer(&i))
	y = y * (1.5 - (n2 * y * y))
	return y
}

// DistanceApprox uses Alpha max plus beta min algorithm (error ~4%)
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

// Sqrt now uses hardware SQRT instructions (~300x faster)
func Sqrt(x int64) int64 {
	if x <= 0 {
		return 0
	}
	// Math derivation: sqrt(x / 2^32) * 2^32  ==  sqrt(x) * 2^16
	// We use 65536.0 constant for 2^16
	return int64(math.Sqrt(float64(x)) * 65536.0)
}

// --- Randomness ---

type FastRand struct {
	state uint64
}

func NewFastRand(seed uint64) *FastRand {
	if seed == 0 {
		seed = 1
	}
	return &FastRand{state: seed}
}

func (r *FastRand) Next() uint64 {
	r.state ^= r.state << 13
	r.state ^= r.state >> 17
	r.state ^= r.state << 5
	return r.state
}

func (r *FastRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Next() % uint64(n))
}

func (r *FastRand) Float64() float64 {
	return float64(r.Next()>>11) / (1 << 53)
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
// Equivalent to ToInt but named for semantic clarity in physics contexts
func GridFromCentered(px, py int64) (int, int) {
	return ToInt(px), ToInt(py)
}