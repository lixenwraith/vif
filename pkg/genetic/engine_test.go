package genetic

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
)

func newBatchEngine(parallelism int, seed uint64) *Engine[[]float64, float64] {
	bounds := make([]ParameterBounds, 16)
	for i := range bounds {
		bounds[i] = ParameterBounds{Min: -1, Max: 1}
	}
	cfg := DefaultConfig()
	cfg.PoolSize = 64
	cfg.Parallelism = parallelism
	cfg.Seed = seed
	return NewEngine[[]float64, float64](
		func(g []float64) float64 {
			s := 0.0
			for _, v := range g {
				s -= v * v
			}
			return s
		},
		func(rng *rand.Rand) []float64 {
			g := make([]float64, len(bounds))
			for i := range g {
				g[i] = rng.Float64()*2 - 1
			}
			return g
		},
		&TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&BoundedPerturbator{Bounds: bounds},
		cfg,
	)
}

// Elites carry over, so the best score never falls between generations
func TestEngine_BestNeverFalls(t *testing.T) {
	e := newBatchEngine(1, 1)
	e.SetTerminator(func(_ *Pool[[]float64, float64], i int) bool { return i == 60 })
	pool, err := e.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := e.History()
	for i := 1; i < len(h); i++ {
		if h[i].BestScore < h[i-1].BestScore {
			t.Fatalf("best fell from %v to %v at generation %d", h[i-1].BestScore, h[i].BestScore, i)
		}
	}
	if pool.Stats.BestScore < h[0].BestScore || pool.Stats.BestScore < -0.5 {
		t.Fatalf("no progress: %v -> %v", h[0].BestScore, pool.Stats.BestScore)
	}
}

// Scoring fans out to workers, but the result is the serial one
func TestEngine_ParallelismDoesNotChangeTheResult(t *testing.T) {
	run := func(parallelism int) []Candidate[[]float64, float64] {
		e := newBatchEngine(parallelism, 2)
		e.SetTerminator(func(_ *Pool[[]float64, float64], i int) bool { return i == 20 })
		pool, err := e.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return pool.Members
	}
	if !reflect.DeepEqual(run(1), run(4)) {
		t.Fatal("parallel evaluation changed the pool")
	}
}

// A cancelled context is read between generations and ends the run with its
// error and the pool so far
func TestEngine_StopsOnCancellation(t *testing.T) {
	e := newBatchEngine(1, 3)
	ctx, cancel := context.WithCancel(context.Background())
	e.SetTerminator(func(p *Pool[[]float64, float64], i int) bool {
		if i == 5 {
			cancel()
		}
		return false
	})
	pool, err := e.Run(ctx)
	if err != context.Canceled || pool.Generation != 6 {
		t.Fatalf("got %v at generation %d, want cancellation after 6", err, pool.Generation)
	}
}

// A generation reuses the buffers of the members it replaced
func TestEngine_GenerationAllocatesNothing(t *testing.T) {
	e := newBatchEngine(1, 4)
	e.initialize()
	e.step()
	if n := testing.AllocsPerRun(20, e.step); n != 0 {
		t.Fatalf("expected 0 allocs per generation, got %v", n)
	}
}

func BenchmarkEngine_Generation(b *testing.B) {
	for name, p := range map[string]int{"serial": 1, "parallel4": 4} {
		b.Run(name, func(b *testing.B) {
			e := newBatchEngine(p, 9)
			e.initialize()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				e.step()
			}
		})
	}
}

// TestEngine_SeedsScheduleAndCheckpoint: seeds open the first generation, a
// schedule moves the strength the perturbator sees, and a run stopped and
// restored from its checkpoint ends where an unbroken run ends.
func TestEngine_SeedsScheduleAndCheckpoint(t *testing.T) {
	seed := []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6}
	fresh := func() *Engine[[]float64, float64] {
		e := newBatchEngine(1, 9)
		e.SetSeeds([][]float64{append([]float64(nil), seed...)})
		return e
	}
	e := fresh()
	e.SetTerminator(func(p *Pool[[]float64, float64], gen int) bool {
		if gen == 0 {
			found := false
			for _, m := range p.Members {
				found = found || reflect.DeepEqual(m.Data, seed)
			}
			if !found {
				t.Error("the seed is not in the first generation")
			}
		}
		return gen == 3
	})
	var strengths []float64
	e.SetSchedule(func(gen int, stats PoolStats[float64]) (float64, float64) {
		strengths = append(strengths, 0.01*float64(gen+1))
		return 0.2, strengths[len(strengths)-1]
	})
	if _, err := e.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(strengths) != 3 || strengths[2] != 0.03 {
		t.Errorf("schedule ran %v", strengths)
	}
	st, err := e.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	st.Members = append([]Candidate[[]float64, float64](nil), st.Members...)
	for i := range st.Members {
		st.Members[i].Data = append([]float64(nil), st.Members[i].Data...)
	}
	rest := fresh()
	if err := rest.Restore(st); err != nil {
		t.Fatal(err)
	}
	rest.SetSchedule(func(gen int, _ PoolStats[float64]) (float64, float64) { return 0.2, 0.01 * float64(gen+1) })
	e.SetTerminator(nil)
	e.SetSchedule(func(gen int, _ PoolStats[float64]) (float64, float64) { return 0.2, 0.01 * float64(gen+1) })
	a, _ := e.Run(context.Background())
	b, _ := rest.Run(context.Background())
	if a.Generation != b.Generation || !reflect.DeepEqual(a.Members, b.Members) {
		t.Errorf("a restored run diverged: generation %d vs %d, best %g vs %g", a.Generation, b.Generation, a.Stats.BestScore, b.Stats.BestScore)
	}
	bad := st
	bad.Version = 99
	if err := fresh().Restore(bad); err == nil {
		t.Error("another layout restored")
	}
}

// TestAdaptiveStrengthFollowsSuccess rises after a better best and falls after
// a stall, within its bounds.
func TestAdaptiveStrengthFollowsSuccess(t *testing.T) {
	s := AdaptiveStrength[float64](0.2, 0.1, 0.01, 0.5, 2)
	_, a := s(0, PoolStats[float64]{BestScore: 1})
	_, b := s(1, PoolStats[float64]{BestScore: 2})
	_, c := s(2, PoolStats[float64]{BestScore: 2})
	_, d := s(3, PoolStats[float64]{BestScore: 2})
	if a != 0.1 || b != 0.2 || c != 0.1 || d != 0.05 {
		t.Errorf("strengths %g %g %g %g", a, b, c, d)
	}
	for i := range 20 {
		_, d = s(4+i, PoolStats[float64]{BestScore: 2})
	}
	if d != 0.01 {
		t.Errorf("fell past the floor to %g", d)
	}
}

// TestMemoizeScoresAKeyOnce counts evaluations under repeats.
func TestMemoizeScoresAKeyOnce(t *testing.T) {
	calls := 0
	eval := Memoize(func(g []int) string { return fmt.Sprint(g) }, func(g []int) float64 {
		calls++
		return float64(len(g))
	})
	for range 3 {
		eval([]int{1, 2})
		eval([]int{2, 1})
	}
	if calls != 2 {
		t.Errorf("%d evaluations for two keys", calls)
	}
}

// TestCategoricalPerturbatorStaysInItsCategories moves elements only to other
// categories of their own count, at the rate asked.
func TestCategoricalPerturbatorStaysInItsCategories(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	cp := &CategoricalPerturbator{Categories: 4, Per: []int{1, 2}}
	moved := 0
	for range 1000 {
		g := []int{0, 1, 3, 2}
		cp.Perturb(&g, 0.5, 0, rng)
		if g[0] != 0 || g[1] < 0 || g[1] > 1 || g[2] < 0 || g[2] > 3 || g[3] < 0 || g[3] > 3 {
			t.Fatalf("left its categories: %v", g)
		}
		if g[2] != 3 {
			moved++
		}
	}
	if moved < 400 || moved > 600 {
		t.Errorf("moved %d of 1000 at rate 0.5", moved)
	}
}
