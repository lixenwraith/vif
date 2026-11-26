package system

import (
	"math"
	"slices"
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

// TestAChainStartsWhereItsHitLanded: a follow-up's origin is the struck member's
// cell, whichever hit chains it, direct or area, never the owner's cell or the
// parent's emitter, so what a chain does from its origin starts at the impact.
func TestAChainStartsWhereItsHitLanded(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 12, Y: 9})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 100,
	})
	chainOrigin := func() (int, int, bool) {
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.CombatAttackDirectRequestPayload); ok && p.ChainDepth > 0 {
				return p.OriginX, p.OriginY, p.HasOrigin
			}
		}
		t.Fatal("the hit chained nothing")
		return 0, 0, false
	}

	combat.applyHitDirect(&event.CombatAttackDirectRequestPayload{
		AttackType: component.CombatAttackTracer, OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntity: target, HasOrigin: true, OriginX: 2, OriginY: 2,
	})
	if x, y, ok := chainOrigin(); !ok || x != 12 || y != 9 {
		t.Fatalf("direct hit's chain starts at (%d,%d) set %v, want the hit's cell (12,9)", x, y, ok)
	}
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		AttackType: component.CombatAttackPulse, OwnerEntity: cursor, OriginEntity: cursor,
		TargetEntity: target, HitEntities: []core.Entity{target}, Charged: true,
		HasOrigin: true, OriginX: 2, OriginY: 2,
	})
	if x, y, ok := chainOrigin(); !ok || x != 12 || y != 9 {
		t.Fatalf("area hit's chain starts at (%d,%d) set %v, want the hit's cell (12,9)", x, y, ok)
	}
}

// TestChargedPulseLeavesABurnCreditedToItsLastRefresher: only a charged pulse
// burns; a second player's charged pulse refreshes the burn and takes its credit;
// the burn ticks BurnDamage every BurnPeriod for BurnDuration, then clears.
func TestChargedPulseLeavesABurnCreditedToItsLastRefresher(t *testing.T) {
	w, first, second := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
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

// TestOnlyAWardBlocksABurnTick: species-authored invulnerability refuses
// a tick, while the window a player's own direct hit spends does not.
func TestOnlyAWardBlocksABurnTick(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
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

	state.Status[component.StatusWard] = component.Status{Remaining: time.Hour}
	combat.Update()
	if state.HitPoints != 100 {
		t.Fatal("a ward let a burn tick through")
	}

	state.Status[component.StatusWard] = component.Status{}
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

// TestAConfuseKicksItsBodyAWayDrawnAnewEachPeriod: a confuse area hit leaves its
// status for ConfuseDuration, kicking the body ConfuseKick cells a second each
// ConfusePeriod, the way differing kick to kick, and nothing once it runs out
func TestAConfuseKicksItsBodyAWayDrawnAnewEachPeriod(t *testing.T) {
	w, first, _ := testCursorWorld(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	target := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(target, component.PositionComponent{X: 8, Y: 5})
	w.Components.Kinetic.SetComponent(target, component.KineticComponent{})
	w.Components.Combat.SetComponent(target, component.CombatComponent{
		OwnerEntity: target, CombatEntityType: component.CombatEntitySwarm, HitPoints: 100,
	})
	combat.applyHitArea(&event.CombatAttackAreaRequestPayload{
		OwnerEntity: first, OriginEntity: first, TargetEntity: target,
		HitEntities: []core.Entity{target}, AttackType: component.CombatAttackConfuse,
	})
	state, _ := w.Components.Combat.GetPtr(target)
	if got := state.Status[component.StatusConfuse].Remaining; got != parameter.ConfuseDuration || state.HitPoints != 100 {
		t.Fatalf("a confuse left %v and %d hit points, want %v and no damage", got, state.HitPoints, parameter.ConfuseDuration)
	}
	w.Resources.Time.DeltaTime = parameter.ConfusePeriod
	kinetic, _ := w.Components.Kinetic.GetPtr(target)
	var ways []float64
	for range int(parameter.ConfuseDuration/parameter.ConfusePeriod) + 2 {
		kinetic.VelX, kinetic.VelY = 0, 0
		w.Resources.Game.State.IncrementGameTicks()
		combat.Update()
		if speed := math.Hypot(kinetic.VelX, kinetic.VelY); speed != 0 {
			if math.Abs(speed-parameter.ConfuseKick) > 0.01 {
				t.Fatalf("a kick of %.2f cells a second, want %v", speed, parameter.ConfuseKick)
			}
			ways = append(ways, math.Atan2(kinetic.VelY, kinetic.VelX))
		}
	}
	if len(ways) != int(parameter.ConfuseDuration/parameter.ConfusePeriod) {
		t.Fatalf("%d kicks over the confuse, want one a period", len(ways))
	}
	if slices.Min(ways) == slices.Max(ways) {
		t.Fatal("every kick went the same way")
	}
}
