package genetic

import (
	"math"
	"math/rand/v2"
	"testing"
)

// Mutation must never leave the declared bounds
func FuzzBoundedPerturbator(f *testing.F) {
	f.Add(uint64(1), 0.5, 0.9, -3.0, 7.0)

	f.Fuzz(func(t *testing.T, seed uint64, rate, strength, lo, hi float64) {
		span := hi - lo
		if hi <= lo || span != span || span > math.MaxFloat64/2 ||
			rate < 0 || rate > 1 || strength < 0 || strength > 4 {
			t.Skip()
		}
		bp := &BoundedPerturbator{Bounds: []ParameterBounds{{Min: lo, Max: hi}}}
		rng := rand.New(rand.NewPCG(seed, seed))
		g := []float64{lo + (hi-lo)/2}

		for range 1000 {
			bp.Perturb(&g, rate, strength, rng)
			if g[0] < lo || g[0] > hi || g[0] != g[0] {
				t.Fatalf("out of bounds: %v not in [%v,%v]", g[0], lo, hi)
			}
		}
	})
}

func TestTournamentSelector_ZeroAlloc(t *testing.T) {
	members := make([]Candidate[[]float64, float64], 32)
	for i := range members {
		members[i] = Candidate[[]float64, float64]{Data: []float64{float64(i)}, Score: float64(i)}
	}
	sel := &TournamentSelector[[]float64, float64]{TournamentSize: 3}
	dst := make([]Candidate[[]float64, float64], 2)
	rng := rand.New(rand.NewPCG(1, 2))

	if n := testing.AllocsPerRun(1000, func() { sel.Select(members, dst, rng) }); n != 0 {
		t.Fatalf("expected 0 allocs, got %v", n)
	}
}

func TestRouletteSelector_NegativeScores(t *testing.T) {
	members := []Candidate[[]float64, float64]{
		{Score: -10}, {Score: -5}, {Score: -1},
	}
	sel := &RouletteSelector[[]float64, float64]{}
	dst := make([]Candidate[[]float64, float64], 100)
	sel.Select(members, dst, rand.New(rand.NewPCG(1, 2)))

	for _, c := range dst {
		if c.Score >= 0 {
			t.Fatalf("selected out-of-set candidate: %v", c.Score)
		}
	}
}
