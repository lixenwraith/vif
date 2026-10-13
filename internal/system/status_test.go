package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// pulseAt lands one pulse area hit on target and resolves the follow-ups it chains
func pulseAt(w *engine.World, combat *CombatSystem, owner, target core.Entity, charged bool) {
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: owner, OriginEntity: owner, TargetEntity: target,
		HitEntities: []core.Entity{target}, AttackType: component.CombatAttackPulse, Charged: charged,
	})
	for _, ev := range w.Resources.Event.Queue.Consume() {
		combat.HandleEvent(ev)
	}
}

// TestChargedPulseLeavesABurnCreditedToItsLastRefresher: only a charged pulse
// burns; a second player's charged pulse refreshes the burn and takes its credit;
// the burn ticks BurnDamage every BurnPeriod for BurnDuration, then clears.
func TestChargedPulseLeavesABurnCreditedToItsLastRefresher(t *testing.T) {
	w, first, second := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 100,
	})
	state, _ := w.Components.Combat.GetPtr(target)

	pulseAt(w, combat, first, target, false)
	if state.Status[component.StatusBurn].Remaining != 0 {
		t.Fatal("an uncharged pulse left a burn")
	}
	pulseAt(w, combat, first, target, true)
	burn := &state.Status[component.StatusBurn]
	if burn.Remaining != parameter.BurnDuration || burn.Source != first || burn.Attack != component.CombatAttackBurn {
		t.Fatalf("burn = %+v, want %v from %d", *burn, parameter.BurnDuration, first)
	}
	state.RemainingDamageImmunity = 0
	burn.Remaining /= 2
	pulseAt(w, combat, second, target, true)
	if burn.Remaining != parameter.BurnDuration || burn.Source != second {
		t.Fatalf("burn = %+v, want refreshed to %v and credited to %d", *burn, parameter.BurnDuration, second)
	}

	hp := state.HitPoints
	ticks := int(parameter.BurnDuration / parameter.BurnPeriod)
	w.Resources.Time.DeltaTime = parameter.BurnPeriod
	for range ticks {
		combat.Update()
	}
	if state.HitPoints != hp-ticks*parameter.BurnDamage || state.LastDamagedBy != second {
		t.Fatalf("hp %d -> %d credited to %d, want %d ticks of %d credited to %d",
			hp, state.HitPoints, state.LastDamagedBy, ticks, parameter.BurnDamage, second)
	}
	if *burn != (component.Status{}) {
		t.Fatalf("burn = %+v after its duration, want cleared", *burn)
	}
}

// TestOnlyASealedWindowBlocksABurnTick: species-authored invulnerability refuses
// a tick, while the window a player's own direct hit spends does not.
func TestOnlyASealedWindowBlocksABurnTick(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	combat := NewCombatSystem(w).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 100,
	})
	state, _ := w.Components.Combat.GetPtr(target)
	state.Status[component.StatusBurn] = component.Status{
		Remaining: time.Hour, Period: time.Second, Timer: time.Second,
		Magnitude: 1, Source: first, Attack: component.CombatAttackBurn,
	}
	w.Resources.Time.DeltaTime = time.Second

	sealDamageImmunity(state, time.Hour)
	combat.Update()
	if state.HitPoints != 100 {
		t.Fatal("a sealed window let a burn tick through")
	}

	state.RemainingDamageImmunity = 0
	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		OwnerEntity: first, OriginEntity: first, TargetEntity: target, HitEntity: target,
		AttackType: component.CombatAttackLightning,
	})
	hp := state.HitPoints
	combat.Update()
	if state.HitPoints != hp-1 {
		t.Fatal("the window a direct hit spent blocked the burn tick")
	}
}
