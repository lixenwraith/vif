package vmath

import "math/bits"

// --- Randomness ---

type FastRand struct {
	state uint64
}

// NewFastRand seeds the generator, mixing the seed through the SplitMix64 finalizer first:
// xorshift64 avalanches poorly on structured seeds, shows up as correlated early draws
func NewFastRand(seed uint64) *FastRand {
	seed ^= seed >> 30
	seed *= 0xbf58476d1ce4e5b9
	seed ^= seed >> 27
	seed *= 0x94d049bb133111eb
	seed ^= seed >> 31
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

// Intn returns a value in [0, n) using Lemire multiply-shift.
// Unbiased; the rejection branch fires with probability ~n/2^64
func (r *FastRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	un := uint64(n)
	hi, lo := bits.Mul64(r.Next(), un)
	if lo < un {
		threshold := -un % un
		for lo < threshold {
			hi, lo = bits.Mul64(r.Next(), un)
		}
	}
	return int(hi)
}

func (r *FastRand) Float64() float64 {
	return float64(r.Next()>>11) / (1 << 53)
}
