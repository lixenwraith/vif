package genetic

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func newTestEngine(t *testing.T, cfg StreamingConfig) *StreamingEngine[[]float64, float64] {
	t.Helper()
	bounds := []ParameterBounds{{Min: 0, Max: 1}}
	e := NewStreamingEngine[[]float64, float64](
		func(rng *rand.Rand) []float64 { return []float64{rng.Float64()} },
		&TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&BoundedPerturbator{Bounds: bounds},
		SliceCloner[[]float64, float64]{},
		cfg,
	)
	e.Start()
	return e
}

// Archive must stay sorted descending and never exceed PoolSize
func TestStreaming_ArchiveInvariant(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.PoolSize = 8
	cfg.Seed = 1
	e := newTestEngine(t, cfg)

	for i := range 500 {
		_, id := e.Propose()
		e.CompleteEvaluation(id, float64(i%37))
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.archive) != cfg.PoolSize {
		t.Fatalf("archive size %d", len(e.archive))
	}
	for i := 1; i < len(e.archive); i++ {
		if e.archive[i-1].Score < e.archive[i].Score {
			t.Fatalf("unsorted at %d", i)
		}
	}
}

// Elites must survive an unbounded stream of inferior outcomes
func TestStreaming_Elitism(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.PoolSize = 8
	cfg.Seed = 2
	e := newTestEngine(t, cfg)

	_, id := e.Propose()
	e.CompleteEvaluation(id, 1000)

	for range 1000 {
		_, id := e.Propose()
		e.CompleteEvaluation(id, 0.1)
	}
	if best := e.Stats().BestScore; best != 1000 {
		t.Fatalf("elite lost: %v", best)
	}
}

// Abandoned and never-completed evaluations must not grow pending without bound
func TestStreaming_PendingBounded(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.PendingCapacity = 16
	cfg.Seed = 3
	e := newTestEngine(t, cfg)

	for range 10_000 {
		e.Propose() // Never completed
	}
	if p := e.Stats().Pending; p > 16 {
		t.Fatalf("pending unbounded: %d", p)
	}
	if e.Stats().Evicted == 0 {
		t.Fatal("expected evictions")
	}
}

// Solutions handed out must not alias engine memory
func TestStreaming_NoAliasing(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.PoolSize = 4
	cfg.Seed = 4
	e := newTestEngine(t, cfg)

	held := make([][]float64, 0, 64)
	for range 64 {
		g, id := e.Propose()
		held = append(held, g)
		e.CompleteEvaluation(id, g[0])
	}

	// Mutating caller copies must not corrupt the archive
	for _, g := range held {
		g[0] = -1
	}
	if e.Stats().BestScore < 0 {
		t.Fatal("archive aliased caller memory")
	}
	for _, c := range e.Snapshot().Members {
		if c.Data[0] == -1 {
			t.Fatal("snapshot aliased caller memory")
		}
	}
}

// Concurrent Propose/Complete must be race-free (run with -race)
func TestStreaming_Concurrent(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.Seed = 5
	e := newTestEngine(t, cfg)

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			for range 2000 {
				g, id := e.Propose()
				if id != 0 {
					e.CompleteEvaluation(id, g[0])
				}
				_ = e.Stats()
			}
		})
	}

	wg.Wait()
}

// Identical seeds must yield identical archives
func TestStreaming_Deterministic(t *testing.T) {
	run := func() float64 {
		cfg := DefaultStreamingConfig()
		cfg.Seed = 0xDECAFBAD
		cfg.TickBudget = time.Hour // Determinism requires a non-binding budget
		e := newTestEngine(t, cfg)
		for range 300 {
			g, id := e.Propose()
			e.CompleteEvaluation(id, g[0])
		}
		return e.Stats().BestScore
	}
	if a, b := run(), run(); a != b {
		t.Fatalf("nondeterministic: %v != %v", a, b)
	}
}

// Stop then Start must resume proposal issuance
func TestStreaming_Restart(t *testing.T) {
	e := newTestEngine(t, DefaultStreamingConfig())
	e.Stop()
	if _, id := e.Propose(); id != 0 {
		t.Fatal("expected zero id while stopped")
	}
	e.Start()
	if _, id := e.Propose(); id == 0 {
		t.Fatal("engine did not restart")
	}
}

func BenchmarkStreaming_ProposeComplete(b *testing.B) {
	cfg := DefaultStreamingConfig()
	cfg.Seed = 7
	bounds := []ParameterBounds{{Min: 0, Max: 1}, {Min: 0, Max: 1}}
	e := NewStreamingEngine[[]float64, float64](
		func(rng *rand.Rand) []float64 { return []float64{rng.Float64(), rng.Float64()} },
		&TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&BoundedPerturbator{Bounds: bounds},
		SliceCloner[[]float64, float64]{},
		cfg,
	)
	e.Start()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		g, id := e.Propose()
		e.CompleteEvaluation(id, g[0])
	}
}
