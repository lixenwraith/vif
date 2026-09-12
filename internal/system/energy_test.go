package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func TestEnergyDeltaClassesRespectPolarity(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	s := NewEnergySystem(w).(*EnergySystem)

	tests := []struct {
		name       string
		current    int64
		delta      int
		percentage bool
		deltaType  component.EnergyDeltaType
		want       int64
	}{
		{name: "positive lightning reward", current: 1000, delta: parameter.LightningEnergyDrainReward, deltaType: component.EnergyDeltaReward, want: 1100},
		{name: "negative loot reward", current: -1000, delta: parameter.LootEnergyRewardValue, deltaType: component.EnergyDeltaReward, want: -11000},
		{name: "positive penalty", current: 1000, delta: 100, deltaType: component.EnergyDeltaPenalty, want: 900},
		{name: "negative penalty", current: -1000, delta: 100, deltaType: component.EnergyDeltaPenalty, want: -900},
		{name: "positive passive", current: 1000, delta: 100, deltaType: component.EnergyDeltaPassive, want: 900},
		{name: "negative passive", current: -1000, delta: 100, deltaType: component.EnergyDeltaPassive, want: -900},
		{name: "positive nugget spend", current: 1000, delta: parameter.NuggetJumpCostPercent, percentage: true, deltaType: component.EnergyDeltaSpend, want: 990},
		{name: "negative nugget spend", current: -1000, delta: parameter.NuggetJumpCostPercent, percentage: true, deltaType: component.EnergyDeltaSpend, want: -990},
		{name: "positive gold spend", current: 1000, delta: parameter.GoldJumpCostPercent, percentage: true, deltaType: component.EnergyDeltaSpend, want: 900},
		{name: "negative gold spend", current: -1000, delta: parameter.GoldJumpCostPercent, percentage: true, deltaType: component.EnergyDeltaSpend, want: -900},
		{name: "positive spend crosses", current: 100, delta: 150, deltaType: component.EnergyDeltaSpend, want: -50},
		{name: "negative spend crosses", current: -100, delta: 150, deltaType: component.EnergyDeltaSpend, want: 50},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			energy, _ := w.Components.Energy.GetPtr(cursor)
			energy.Current = tc.current
			s.HandleEvent(event.GameEvent{
				Type: event.EventEnergyAddRequest,
				Payload: &event.EnergyAddPayload{
					Entity: cursor, Delta: tc.delta, Percentage: tc.percentage, Type: tc.deltaType,
				},
			})
			if energy.Current != tc.want {
				t.Fatalf("energy = %d, want %d", energy.Current, tc.want)
			}
			w.Resources.Event.Queue.Consume()
		})
	}
}
