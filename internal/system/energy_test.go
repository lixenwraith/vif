package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
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

// TestChainedEnergyDrainZapsBetweenOwnerAndTarget pins the geometry a cleaner hit
// produces: the chain is authored by the cursor, so the zap spans cursor to
// species instead of collapsing onto the projectile's impact cell.
func TestChainedEnergyDrainZapsBetweenOwnerAndTarget(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)

	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 10, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntityDrain, HitPoints: 100,
	})

	// The impact cell a cleaner reports: the species cell, not the cursor's.
	combat.HandleEvent(event.GameEvent{
		Type: event.EventCombatAttackDirectRequest,
		Payload: &event.CombatAttackDirectRequestPayload{
			AttackType:   component.CombatAttackProjectile,
			OwnerEntity:  cursor,
			OriginEntity: cursor,
			TargetEntity: target,
			HitEntity:    target,
			HasOrigin:    true,
			OriginX:      10,
			OriginY:      5,
		},
	})

	var chain *event.CombatAttackDirectRequestPayload
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.CombatAttackDirectRequestPayload); ok &&
			p.AttackType == component.CombatAttackLightning {
			chain = p
		}
	}
	if chain == nil {
		t.Fatal("projectile hit produced no chained lightning attack")
	}
	combat.HandleEvent(event.GameEvent{Type: event.EventCombatAttackDirectRequest, Payload: chain})

	cursorPos, _ := w.Positions.GetPosition(cursor)
	for _, ev := range w.Resources.Event.Queue.Consume() {
		p, ok := ev.Payload.(*event.LightningSpawnRequestPayload)
		if !ok {
			continue
		}
		if p.OriginX != cursorPos.X || p.OriginY != cursorPos.Y || p.TargetEntity != target {
			t.Fatalf("zap = (%d,%d)->entity %d, want cursor (%d,%d)->species %d",
				p.OriginX, p.OriginY, p.TargetEntity, cursorPos.X, cursorPos.Y, target)
		}
		return
	}
	t.Fatal("energy drain spawned no zap")
}
