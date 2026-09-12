package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func TestEnergyRewardKeepsPolarityUntilDrainReachesZero(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	s := NewEnergySystem(w).(*EnergySystem)
	energy, _ := w.Components.Energy.GetPtr(cursor)

	energy.Current = -1000
	s.addEnergy(cursor, parameter.LootEnergyRewardValue, false, component.EnergyDeltaReward)
	if got, want := energy.Current, int64(-1000-parameter.LootEnergyRewardValue); got != want {
		t.Fatalf("negative reward = %d, want %d", got, want)
	}

	energy.Current = -50
	s.addEnergy(cursor, 100, false, component.EnergyDeltaPassive)
	if energy.Current != 0 {
		t.Fatalf("drained energy = %d, want zero", energy.Current)
	}
	s.addEnergy(cursor, parameter.LootEnergyRewardValue, false, component.EnergyDeltaReward)
	if got, want := energy.Current, int64(parameter.LootEnergyRewardValue); got != want {
		t.Fatalf("reward after zero = %d, want %d", got, want)
	}
}
