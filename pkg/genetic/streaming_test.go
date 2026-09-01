package genetic

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"
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

// Identical seeds determine the complete proposal stream. The nanosecond budget
// is deliberately impossible: deterministic refill is the default and never
// consults it.
func TestStreaming_Deterministic(t *testing.T) {
	run := func() ([]float64, StreamingState[[]float64, float64]) {
		cfg := DefaultStreamingConfig()
		cfg.Seed = 0xDECAFBAD
		cfg.TickBudget = 1
		e := newTestEngine(t, cfg)
		stream := make([]float64, 0, 300)
		for range 300 {
			g, id := e.Propose()
			stream = append(stream, g[0])
			e.CompleteEvaluation(id, g[0])
		}
		state, err := e.Checkpoint()
		if err != nil {
			t.Fatal(err)
		}
		return stream, state
	}
	aStream, aState := run()
	bStream, bState := run()
	if !reflect.DeepEqual(aStream, bStream) {
		t.Fatal("identical seeds produced different proposal streams")
	}
	if !reflect.DeepEqual(aState, bState) {
		t.Fatal("identical seeded operation sequences produced different states")
	}
}

// TestStreaming_CheckpointContinuesTheExactStream exercises every value that an
// archive-only snapshot used to lose: PCG position, queued offspring, pending
// genotypes, partial-generation outcomes, and the next evaluation id.
func TestStreaming_CheckpointContinuesTheExactStream(t *testing.T) {
	cfg := DefaultStreamingConfig()
	cfg.Seed = 0x51A7E
	cfg.PoolSize = 8
	cfg.ProposalCapacity = 8
	cfg.PendingCapacity = 16
	cfg.MinOutcomesPerGen = 3

	source := newTestEngine(t, cfg)
	for i := range 15 {
		g, id := source.Propose()
		switch i % 4 {
		case 0, 1:
			source.CompleteEvaluation(id, g[0]+float64(i)/100)
		case 2:
			source.AbandonEvaluation(id)
		case 3:
			// Deliberately leave work in flight.
		}
	}

	checkpoint, err := source.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var decoded StreamingState[[]float64, float64]
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	restored := newTestEngine(t, cfg)
	for range 5 { // Prove Restore replaces receiver-local state.
		g, id := restored.Propose()
		restored.CompleteEvaluation(id, -g[0])
	}
	if err := restored.Restore(decoded); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checkpoint, got) {
		t.Fatal("restored engine does not equal its checkpoint")
	}

	// Resolve the evaluations that crossed the checkpoint, then compare a long
	// mixture of completion and abandonment. IDs and genotypes must agree at every
	// issuance, not merely converge on the same best score.
	for _, pending := range checkpoint.Pending {
		score := pending.Data[0]
		source.CompleteEvaluation(pending.ID, score)
		restored.CompleteEvaluation(pending.ID, score)
	}
	for i := range 250 {
		a, aid := source.Propose()
		b, bid := restored.Propose()
		if aid != bid || !reflect.DeepEqual(a, b) {
			t.Fatalf("proposal %d differs: (%d, %v) != (%d, %v)", i, aid, a, bid, b)
		}
		if i%7 == 0 {
			source.AbandonEvaluation(aid)
			restored.AbandonEvaluation(bid)
		} else {
			score := a[0] + float64(i%11)/100
			source.CompleteEvaluation(aid, score)
			restored.CompleteEvaluation(bid, score)
		}
	}
	aState, err := source.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	bState, err := restored.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(aState, bState) {
		t.Fatal("restored engine diverged after identical operations")
	}
}

func TestStreaming_RestoreRejectsInvalidStateWithoutMutation(t *testing.T) {
	e := newTestEngine(t, DefaultStreamingConfig())
	before, err := e.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	badVersion := before
	badVersion.Version++
	badConfig := before
	badConfig.Config.PoolSize++
	for name, bad := range map[string]StreamingState[[]float64, float64]{
		"version": badVersion,
		"config":  badConfig,
	} {
		t.Run(name, func(t *testing.T) {
			if err := e.Restore(bad); err == nil {
				t.Fatal("invalid checkpoint was accepted")
			}
			after, err := e.Checkpoint()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed restore changed the engine")
			}
		})
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
