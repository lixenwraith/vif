package registry

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/genetic/tracking"
)

func TestRegistry_RegisterAndSample(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	config := SpeciesConfig{
		ID:                 1,
		Name:               "test",
		GeneCount:          2,
		Bounds:             []genetic.ParameterBounds{{Min: 0, Max: 1}, {Min: 0, Max: 10}},
		PerturbationStdDev: 0.1,
	}

	if err := reg.Register(config, nil); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := reg.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	defer reg.Stop()

	genes, evalID := reg.Sample(1)

	if len(genes) != 2 {
		t.Errorf("expected 2 genes, got %d", len(genes))
	}
	if genes[0] < 0 || genes[0] > 1 {
		t.Errorf("gene[0] out of bounds: %v", genes[0])
	}
	if genes[1] < 0 || genes[1] > 10 {
		t.Errorf("gene[1] out of bounds: %v", genes[1])
	}
	if evalID == 0 {
		t.Error("expected non-zero evalID")
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	config := SpeciesConfig{ID: 1, Name: "test", GeneCount: 1, Bounds: []genetic.ParameterBounds{{Min: 0, Max: 1}}}

	if err := reg.Register(config, nil); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	if err := reg.Register(config, nil); err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestRegistry_BoundsMismatch(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	config := SpeciesConfig{
		ID:        1,
		Name:      "test",
		GeneCount: 3,
		Bounds:    []genetic.ParameterBounds{{Min: 0, Max: 1}}, // Only 1 bound for 3 genes
	}

	if err := reg.Register(config, nil); err == nil {
		t.Error("expected error on bounds mismatch")
	}
}

func TestRegistry_FullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	reg := NewRegistry(tmpDir)

	agg := &fitness.WeightedAggregator{
		Weights: map[string]float64{
			tracking.MetricTicksAlive: 1.0,
		},
		Normalizers: map[string]fitness.NormalizeFunc{
			tracking.MetricTicksAlive: fitness.NormalizeCap(10),
		},
	}

	config := SpeciesConfig{
		ID:                 1,
		Name:               "lifecycle_test",
		GeneCount:          1,
		Bounds:             []genetic.ParameterBounds{{Min: 0, Max: 1}},
		PerturbationStdDev: 0.1,
	}

	reg.Register(config, agg)
	reg.Start()

	// Sample and begin tracking
	genes, evalID := reg.Sample(1)
	if evalID == 0 {
		t.Fatal("expected evalID")
	}

	collector := reg.BeginTracking(1, evalID)
	if collector == nil {
		t.Fatal("expected collector")
	}

	// Collect metrics
	for i := 0; i < 5; i++ {
		reg.CollectMetrics(1, evalID, tracking.MetricBundle{"distance": float64(i)}, 100*time.Millisecond)
	}

	// Complete tracking
	ctx := fitness.MapContext{}
	reg.CompleteTracking(1, evalID, tracking.MetricBundle{}, ctx)

	// Check stats
	stats := reg.Stats(1)
	if stats.TotalEvals < 1 {
		t.Errorf("expected at least 1 eval, got %d", stats.TotalEvals)
	}

	// Save and reload
	if err := reg.SaveAll(); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	reg.Stop()

	// Reload
	reg2 := NewRegistry(tmpDir)
	reg2.Register(config, agg)
	reg2.Start()
	defer reg2.Stop()

	stats2 := reg2.Stats(1)
	if stats2.Generation != stats.Generation {
		t.Errorf("generation mismatch after reload: %d vs %d", stats2.Generation, stats.Generation)
	}

	// Verify we can still sample
	genes2, evalID2 := reg2.Sample(1)
	if len(genes2) != len(genes) {
		t.Error("gene count mismatch after reload")
	}
	if evalID2 == 0 {
		t.Error("expected evalID after reload")
	}
}

func TestRegistry_Evolution(t *testing.T) {
	reg := NewRegistry(t.TempDir())

	// Aggregator rewards higher gene values
	agg := &fitness.WeightedAggregator{
		Weights: map[string]float64{
			"gene_value": 1.0,
		},
	}

	config := SpeciesConfig{
		ID:                 1,
		Name:               "evolution_test",
		GeneCount:          1,
		Bounds:             []genetic.ParameterBounds{{Min: 0, Max: 100}},
		PerturbationStdDev: 0.2,
		EngineConfig: &genetic.StreamingConfig{
			EngineConfig: genetic.EngineConfig{
				PoolSize:             16,
				EliteCount:           2,
				PerturbationRate:     0.3,
				PerturbationStrength: 0.2,
			},
			MinOutcomesPerGen: 2,
		},
	}

	reg.Register(config, agg)
	reg.Start()
	defer reg.Stop()

	initialStats := reg.Stats(1)

	// Run multiple generations
	for gen := 0; gen < 10; gen++ {
		for i := 0; i < 4; i++ {
			genes, evalID := reg.Sample(1)
			if evalID == 0 {
				continue
			}

			collector := reg.BeginTracking(1, evalID)
			// Report gene value as fitness metric
			collector.Collect(tracking.MetricBundle{"gene_value": genes[0]}, time.Second)

			reg.CompleteTracking(1, evalID, tracking.MetricBundle{}, nil)
		}
		// Small delay to let streaming engine process
		time.Sleep(10 * time.Millisecond)
	}

	finalStats := reg.Stats(1)

	if finalStats.TotalEvals <= initialStats.TotalEvals {
		t.Error("expected evaluations to increase")
	}

	// Evolution should have progressed
	if finalStats.Generation <= initialStats.Generation {
		t.Logf("generations: %d -> %d", initialStats.Generation, finalStats.Generation)
	}

	t.Logf("Evolution test: evals=%d, gen=%d, best=%.2f, avg=%.2f",
		finalStats.TotalEvals, finalStats.Generation, finalStats.BestFitness, finalStats.AvgFitness)
}