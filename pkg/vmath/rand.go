package vmath

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
