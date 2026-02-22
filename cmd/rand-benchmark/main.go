// FILE: cmd/bench_rand/main.go
package main

import (
	"fmt"
	rand1 "math/rand"
	rand2 "math/rand/v2"
	"testing"
)

// FastRand — copy of vmath.FastRand (xorshift64: 13, 17, 5)
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

	// x := r.state
	// x ^= x << 13
	// x ^= x >> 17
	// x ^= x << 5
	// r.state = x
	// return x
}

func (r *FastRand) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Next() % uint64(n))
}

// SlicesXorshift — stdlib slices variant (xorshift64: 13, 7, 17)
type SlicesXorshift uint64

func (r *SlicesXorshift) Next() uint64 {
	*r ^= *r << 13
	*r ^= *r >> 7
	*r ^= *r << 17
	return uint64(*r)
}

func (r *SlicesXorshift) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Next() % uint64(n))
}

// SlicesXorshiftSource wraps SlicesXorshift as rand/v2.Source
type SlicesXorshiftSource struct {
	state SlicesXorshift
}

func (s *SlicesXorshiftSource) Uint64() uint64 {
	return s.state.Next()
}

func main() {
	const n = 100
	const bound = 1000

	benchmarks := []struct {
		name string
		fn   func(b *testing.B)
	}{
		{"FastRand.Intn (13,17,5)", func(b *testing.B) {
			rng := NewFastRand(12345)
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rng.Intn(bound)
				}
			}
		}},
		{"SlicesXorshift.Intn (13,7,17)", func(b *testing.B) {
			rng := SlicesXorshift(12345)
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rng.Intn(bound)
				}
			}
		}},
		{"SlicesXorshift via rand/v2.IntN", func(b *testing.B) {
			rng := rand2.New(&SlicesXorshiftSource{state: 12345})
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rng.IntN(bound)
				}
			}
		}},
		{"math/rand.Global.Intn", func(b *testing.B) {
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rand1.Intn(bound)
				}
			}
		}},
		{"math/rand.Source.Intn", func(b *testing.B) {
			rng := rand1.New(rand1.NewSource(12345))
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rng.Intn(bound)
				}
			}
		}},
		{"math/rand/v2.Global.IntN (ChaCha8)", func(b *testing.B) {
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rand2.IntN(bound)
				}
			}
		}},
		{"math/rand/v2.PCG.IntN", func(b *testing.B) {
			rng := rand2.New(rand2.NewPCG(12345, 67890))
			for b.Loop() {
				for i := 0; i < n; i++ {
					_ = rng.IntN(bound)
				}
			}
		}},
	}

	fmt.Printf("Benchmark: %d calls per iteration, bound=%d\n\n", n, bound)
	fmt.Printf("%-40s %12s %12s\n", "Name", "ns/op", "ns/call")
	fmt.Println("--------------------------------------------------------------")

	for _, bm := range benchmarks {
		result := testing.Benchmark(bm.fn)
		nsPerOp := float64(result.T.Nanoseconds()) / float64(result.N)
		nsPerCall := nsPerOp / float64(n)
		fmt.Printf("%-40s %10.1f ns %9.2f ns\n", bm.name, nsPerOp, nsPerCall)
	}

	// Verify the two xorshift variants produce different sequences
	fmt.Println("\nSequence divergence (seed=42, 5 values, bound=100):")
	fr := NewFastRand(42)
	sx := SlicesXorshift(42)
	fmt.Print("  FastRand (13,17,5):    ")
	for i := 0; i < 5; i++ {
		fmt.Printf("%3d ", fr.Intn(100))
	}
	fmt.Print("\n  SlicesXS (13,7,17):    ")
	for i := 0; i < 5; i++ {
		fmt.Printf("%3d ", sx.Intn(100))
	}
	fmt.Println()
}