package registry

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/genetic"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/fitness"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/persistence"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/tracking"
)

func TestRegistry_RegisterAndSample(t *testing.T) {
	reg := NewRegistry(nil)

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
	reg := NewRegistry(nil)

	config := SpeciesConfig{ID: 1, Name: "test", GeneCount: 1, Bounds: []genetic.ParameterBounds{{Min: 0, Max: 1}}}

	if err := reg.Register(config, nil); err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	if err := reg.Register(config, nil); err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestRegistry_BoundsMismatch(t *testing.T) {
	reg := NewRegistry(nil)

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
	store := persistence.NewManager(t.TempDir(), nil)
	reg := NewRegistry(store)

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
	for i := range 5 {
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
	reg2 := NewRegistry(store)
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
	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize = 16
	cfg.MinOutcomesPerGen = 2
	cfg.PerturbationRate = 0.6
	cfg.PerturbationStrength = 0.15
	cfg.Seed = 0xC0FFEE

	reg := NewRegistry(nil)
	config := SpeciesConfig{
		ID:             1,
		Name:           "evolution_test",
		GeneCount:      1,
		Bounds:         []genetic.ParameterBounds{{Min: 0, Max: 100}},
		TournamentSize: 3,
		MixProbability: 0.5,
		EngineConfig:   &cfg,
	}
	if err := reg.Register(config, nil); err != nil {
		t.Fatal(err)
	}
	reg.Start()
	defer reg.Stop()

	// Fitness is the gene value: the archive must drift toward the upper bound
	var early, late float64
	for i := range 400 {
		genes, evalID := reg.Sample(1)
		if evalID == 0 {
			t.Fatal("expected proposal")
		}
		reg.ReportFitness(1, evalID, genes[0])

		if i == 40 {
			early = reg.Stats(1).AvgFitness
		}
	}
	late = reg.Stats(1).AvgFitness

	st := reg.Stats(1)
	if st.PoolSize != cfg.PoolSize {
		t.Fatalf("archive not saturated: %d", st.PoolSize)
	}
	if st.BestFitness < st.AvgFitness || st.AvgFitness < st.WorstFitness {
		t.Fatalf("archive not sorted: best=%v avg=%v worst=%v",
			st.BestFitness, st.AvgFitness, st.WorstFitness)
	}
	if late <= early {
		t.Fatalf("no convergence: %v -> %v", early, late)
	}
	if st.BestFitness < 90 {
		t.Errorf("expected convergence near upper bound, got %v", st.BestFitness)
	}
	if st.PendingCount != 0 {
		t.Errorf("expected no pending evaluations, got %d", st.PendingCount)
	}
}

func TestRegistry_ExportImportContinuesSamplesAndScouts(t *testing.T) {
	cfg := genetic.DefaultStreamingConfig()
	cfg.Seed = 0xC01A71
	cfg.PoolSize = 8
	cfg.ProposalCapacity = 8
	cfg.PendingCapacity = 32
	cfg.MinOutcomesPerGen = 3

	newRegistry := func() *Registry {
		reg := NewRegistry(nil)
		err := reg.Register(SpeciesConfig{
			ID:             3,
			Name:           "continuation",
			GeneCount:      2,
			Bounds:         []genetic.ParameterBounds{{Min: 0, Max: 1}, {Min: -2, Max: 2}},
			ProbeBins:      5,
			TournamentSize: 3,
			MixProbability: 0.5,
			EngineConfig:   &cfg,
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Start(); err != nil {
			t.Fatal(err)
		}
		return reg
	}

	source := newRegistry()
	defer source.Stop()
	for i := range 24 {
		var genes []float64
		var id uint64
		if i%3 == 0 {
			genes, id = source.SampleScout(3)
		} else {
			genes, id = source.Sample(3)
		}
		if i%4 == 0 {
			source.AbandonFitness(3, id)
		} else if i%4 != 3 { // Keep one quarter pending across the checkpoint.
			source.ReportFitness(3, id, genes[0]+genes[1]/10)
		}
	}

	state, err := source.Export()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []SpeciesState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}

	restored := newRegistry()
	defer restored.Stop()
	for range 7 {
		genes, id := restored.Sample(3)
		restored.ReportFitness(3, id, -genes[0])
	}
	if err := restored.Import(decoded); err != nil {
		t.Fatal(err)
	}
	got, err := restored.Export()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, got) {
		t.Fatal("registry import did not reproduce the exported state")
	}

	for i := range 180 {
		var a, b []float64
		var aid, bid uint64
		if i%4 == 0 {
			a, aid = source.SampleScout(3)
			b, bid = restored.SampleScout(3)
		} else {
			a, aid = source.Sample(3)
			b, bid = restored.Sample(3)
		}
		if aid != bid || !reflect.DeepEqual(a, b) {
			t.Fatalf("sample %d differs: (%d, %v) != (%d, %v)", i, aid, a, bid, b)
		}
		if i%6 == 0 {
			source.AbandonFitness(3, aid)
			restored.AbandonFitness(3, bid)
		} else {
			score := a[0] + a[1]/10
			source.ReportFitness(3, aid, score)
			restored.ReportFitness(3, bid, score)
		}
	}
	finalA, err := source.Export()
	if err != nil {
		t.Fatal(err)
	}
	finalB, err := restored.Export()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(finalA, finalB) {
		t.Fatal("restored registry diverged after identical use")
	}
}
