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

// State returns the generator's position, so a test can assert a code path drew
// nothing from a stream. Never zero: NewFastRand rejects a zero seed.
func (r *FastRand) State() uint64 { return r.state }

// SetState resumes the generator at a position State reported, which is what lets
// a stream continue across a transfer rather than restart. A seed reproduces a
// sequence from its beginning; only the position reproduces it from where a run
// had reached, and a snapshot has to do the second.
//
// Zero is rejected the same way NewFastRand rejects a zero seed: xorshift64 has a
// fixed point there and would return zero forever. A caller handing over an
// uninitialized field gets a live generator rather than a dead one.
func (r *FastRand) SetState(state uint64) {
	if state == 0 {
		state = 1
	}
	r.state = state
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

// NewSeededRand returns a generator for the labelled stream of a root seed.
func NewSeededRand(root uint64, label string) *FastRand {
	return NewFastRand(DeriveSeed(root, label))
}

// DeriveSeed produces an independent, reproducible stream seed from a root seed and
// a label. Splitting by label keeps one system's draws from shifting another's when
// call counts change.
func DeriveSeed(root uint64, label string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(label); i++ {
		h ^= uint64(label[i])
		h *= 1099511628211
	}
	x := root + h
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	if x == 0 {
		x = 0x9e3779b97f4a7c15
	}
	return x
}
