package game

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/genetic/game/species"
)

func TestGeneticResource_SampleAndDecode(t *testing.T) {
	res := NewGeneticResource()
	res.Start()
	defer res.Stop()

	genes, evalID := res.Sample(component.SpeciesDrain)

	if len(genes) != species.DrainGeneCount {
		t.Errorf("expected %d genes, got %d", species.DrainGeneCount, len(genes))
	}
	if evalID == 0 {
		t.Error("expected non-zero evalID")
	}

	phenotype := res.Decode(component.SpeciesDrain, genes)
	if phenotype == nil {
		t.Fatal("expected phenotype")
	}

	dp, ok := phenotype.(species.DrainPhenotype)
	if !ok {
		t.Fatalf("expected DrainPhenotype, got %T", phenotype)
	}

	if dp.HomingAccel == 0 {
		t.Error("expected non-zero HomingAccel")
	}
	if dp.AggressionMult == 0 {
		t.Error("expected non-zero AggressionMult")
	}
}

func TestGeneticResource_Stats(t *testing.T) {
	res := NewGeneticResource()
	res.Start()
	defer res.Stop()

	// Complete a few evaluations
	for i := 0; i < 3; i++ {
		_, evalID := res.Sample(component.SpeciesDrain)
		if evalID != 0 {
			res.Complete(component.SpeciesDrain, evalID, 0.5)
		}
	}

	stats := res.Stats(component.SpeciesDrain)

	if stats.OutcomesTotal < 1 {
		t.Errorf("expected at least 1 outcome, got %d", stats.OutcomesTotal)
	}
}

func TestGeneticResource_PlayerModel(t *testing.T) {
	res := NewGeneticResource()

	model := res.PlayerModel()
	if model == nil {
		t.Fatal("expected player model")
	}

	model.RecordEnergyLevel(5000)
	model.RecordHeatLevel(50, 100)
	model.RecordKeystroke(true)

	ctx := res.PlayerContext()

	threat, ok := ctx.Get("threat_level")
	if !ok {
		t.Error("expected threat_level in context")
	}
	if threat <= 0 || threat > 1 {
		t.Errorf("threat_level out of range: %v", threat)
	}
}

func TestGeneticResource_Reset(t *testing.T) {
	res := NewGeneticResource()
	res.Start()

	model := res.PlayerModel()
	model.RecordHeatLevel(100, 100)

	snap1 := model.Snapshot()

	res.Reset()

	snap2 := model.Snapshot()

	if snap2.HeatManagement == snap1.HeatManagement && snap1.HeatManagement != 0.3 {
		t.Error("expected player model to reset")
	}
}