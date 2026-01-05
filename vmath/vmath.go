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
	quo, _ := bits.Div64(hi, lo, ub)

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

// --- 2D Traversal (Supercover DDA) ---

// Traverse visits every grid cell intersected by a line from (x1, y1) to (x2, y2), coordinates are Q32.32 fixed point
// Uses Supercover DDA to ensure no skipped cells, guaranteed to terminate by checking target bounds before stepping
func Traverse(x1, y1, x2, y2 int64, callback func(x, y int) bool) {
	ix, iy := ToInt(x1), ToInt(y1)
	targetX, targetY := ToInt(x2), ToInt(y2)

	if ix == targetX && iy == targetY {
		callback(ix, iy)
		return
	}

	dx := x2 - x1
	dy := y2 - y1

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
		dx = -dx
	}
	if dy < 0 {
		stepY = -1
		dy = -dy
	}

	// Calculate initial tMax and tDelta
	var tMaxX, tMaxY, tDeltaX, tDeltaY int64
	if dx == 0 {
		tMaxX = math.MaxInt64
	} else {
		tDeltaX = Div(Scale, dx)
		if stepX > 0 {
			tMaxX = Mul(Scale-(x1&Mask), tDeltaX)
		} else {
			tMaxX = Mul(x1&Mask, tDeltaX)
		}
	}

	if dy == 0 {
		tMaxY = math.MaxInt64
	} else {
		tDeltaY = Div(Scale, dy)
		if stepY > 0 {
			tMaxY = Mul(Scale-(y1&Mask), tDeltaY)
		} else {
			tMaxY = Mul((y1 & Mask), tDeltaY)
		}
	}

	if !callback(ix, iy) {
		return
	}

	// Loop until both indices match targets
	for ix != targetX || iy != targetY {
		if tMaxX < tMaxY {
			// Try stepping X
			if ix != targetX {
				ix += stepX
				tMaxX += tDeltaX
			} else {
				// X is done, forced to step Y
				iy += stepY
				tMaxY += tDeltaY
			}
		} else if tMaxX > tMaxY {
			// Try stepping Y
			if iy != targetY {
				iy += stepY
				tMaxY += tDeltaY
			} else {
				// Y is done, forced to step X
				ix += stepX
				tMaxX += tDeltaX
			}
		} else {
			// Diagonal step (tMaxX == tMaxY)
			if ix != targetX {
				ix += stepX
				tMaxX += tDeltaX
			}
			if iy != targetY {
				iy += stepY
				tMaxY += tDeltaY
			}
		}

		if !callback(ix, iy) {
			break
		}
	}
}

// CalculateCentroid computes the geometric center of a set of 2D points
// Returns (0,0) if the input slice is empty
// coords contains interleaved X,Y values (len must be even)
func CalculateCentroid(coords []int) (int, int) {
	if len(coords) == 0 || len(coords)%2 != 0 {
		return 0, 0
	}

	sumX, sumY := 0, 0
	count := len(coords) / 2

	for i := 0; i < len(coords); i += 2 {
		sumX += coords[i]
		sumY += coords[i+1]
	}

	return sumX / count, sumY / count
}

// Lerp performs linear interpolation between a and b
// t is in [0, Scale] where 0 returns a, Scale returns b
func Lerp(a, b, t int64) int64 {
	return a + Mul(b-a, t)
}