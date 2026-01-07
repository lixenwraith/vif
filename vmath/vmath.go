package vmath

import (
	"math"
	"math/bits"
	"unsafe"
)

// Q32.32 Fixed Point constants
const (
	Shift   = 32
	Scale   = 1 << Shift
	Mask    = Scale - 1
	Half    = 1 << (Shift - 1)
	LUTSize = 1024
	LUTMask = LUTSize - 1
)

// --- Arithmetic ---

func FromInt(i int) int64       { return int64(i << Shift) }
func ToInt(f int64) int         { return int(f >> Shift) }
func FromFloat(f float64) int64 { return int64(f * Scale) }
func ToFloat(f int64) float64   { return float64(f) / Scale }

func Mul(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	negative := (a < 0) != (b < 0)
	ua, ub := uint64(a), uint64(b)
	if a < 0 {
		ua = uint64(-a)
	}
	if b < 0 {
		ub = uint64(-b)
	}

	hi, lo := bits.Mul64(ua, ub)
	// Q32.32 * Q32.32 = Q64.64, shift right 32 for Q32.32
	result := int64((hi << 32) | (lo >> 32))

	if negative {
		return -result
	}
	return result
}

func Div(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	negative := (a < 0) != (b < 0)
	ua, ub := uint64(a), uint64(b)
	if a < 0 {
		ua = uint64(-a)
	}
	if b < 0 {
		ub = uint64(-b)
	}

	// a << 32 as 128-bit: hi = a >> 32, lo = a << 32
	hi := ua >> 32
	lo := ua << 32

	// Protection against overflow: if hi >= ub, the quotient will not fit in 64 bits
	// This happens if |a| * Scale >= |b| * 2^64 (e.g. a=Scale, b=1)
	if hi >= ub {
		if negative {
			return math.MinInt64
		}
		return math.MaxInt64
	}

	quo, _ := bits.Div64(hi, lo, ub)

	// Saturate if the result exceeds int64 range
	if quo > math.MaxInt64 {
		// Special case: -2^63 is representable
		if negative && quo == 1<<63 {
			return math.MinInt64
		}
		if negative {
			return math.MinInt64
		}
		return math.MaxInt64
	}

	if negative {
		return -int64(quo)
	}
	return int64(quo)
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

// MulDiv computes (a * b) / c with 128-bit intermediate
// Useful for ratio calculations without precision loss
func MulDiv(a, b, c int64) int64 {
	if c == 0 {
		return 0
	}
	neg := ((a < 0) != (b < 0)) != (c < 0)
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	if c < 0 {
		c = -c
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	q, _ := bits.Div64(hi, lo, uint64(c))
	r := int64(q)
	if neg {
		return -r
	}
	return r
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

// Sqrt returns Q32.32 square root using Newton-Raphson
// For non-performance-critical paths with large values, prefer math.Sqrt for accuracy
// This implementation converges in 8 iterations for typical game distances (0-500 units)
// For values > 1000 units or when precision is critical, use:
//
//	result := vmath.FromFloat(math.Sqrt(vmath.ToFloat(x)))
func Sqrt(x int64) int64 {
	if x <= 0 {
		return 0
	}

	// Better initial guess using bit manipulation
	// Find highest set bit position, estimate sqrt from that
	guess := x
	if guess > Scale {
		// For values > 1.0, start closer to sqrt
		guess = Scale // Start at 1.0 in Q32.32
		for guess < x>>1 {
			guess <<= 1
		}
	} else {
		guess = x >> 1
		if guess == 0 {
			guess = 1
		}
	}

	// 12 iterations for Q32.32 precision across typical ranges (vs. 8 for Q32.32)
	for i := 0; i < 12; i++ {
		if guess == 0 {
			return 0
		}
		guess = (guess + Div(x, guess)) >> 1
	}
	return guess
}

// --- Randomness ---

// TODO: maybe just use sort.xorshift? it's uint64
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
	x := r.state
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.state = x
	return x
}

func (r *FastRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Next() % uint64(n))
}