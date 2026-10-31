package genetic

import (
	"context"
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
