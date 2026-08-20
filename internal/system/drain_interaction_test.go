package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

func testDrainInteractionWorld(
	t *testing.T,
	cursorX, cursorY, drainX, drainY int,
) (*engine.World, core.Entity, core.Entity, *DrainSystem, *CombatSystem) {
	t.Helper()

	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())

	cursors := NewCursorSystem(w).(*CursorSystem)
	cursors.HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: cursorX, Y: cursorY, Slot: 0, Control: uint8(component.ControlHuman),
		},
	})
	cursor := w.Resources.Player.Slot(0)
	if cursor == 0 {
		t.Fatal("cursor was not created")
	}
	w.Resources.Event.Queue.Consume()

	drains := NewDrainSystem(w).(*DrainSystem)
	drains.materializeDrainAt(drainX, drainY)
	drainEntities := w.Components.Drain.Entities()
	if len(drainEntities) != 1 {
		t.Fatalf("drain count = %d, want 1", len(drainEntities))
	}
	drain := drainEntities[0]
	w.Resources.Event.Queue.Consume()

	return w, cursor, drain, drains, NewCombatSystem(w).(*CombatSystem)
}

func applyQueuedCombatArea(t *testing.T, w *engine.World, combat *CombatSystem) {
	t.Helper()

	hits := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventCombatAttackAreaRequest {
			continue
		}
		combat.HandleEvent(ev)
		hits++
	}
	if hits != 1 {
		t.Fatalf("combat area events = %d, want 1", hits)
	}
}

func assertDrainKnockbackIntegrated(
	t *testing.T,
	w *engine.World,
	drain core.Entity,
	drains *DrainSystem,
) {
	t.Helper()

	before, ok := w.Components.Kinetic.GetComponent(drain)
	if !ok {
		t.Fatal("drain has no kinetic component")
	}
	combat, ok := w.Components.Combat.GetComponent(drain)
	if !ok || combat.RemainingKineticImmunity <= 0 {
		t.Fatalf("kinetic immunity = %v, want active knockback window", combat.RemainingKineticImmunity)
	}
	if before.VelX == 0 && before.VelY == 0 {
		t.Fatal("combat did not apply a knockback impulse")
	}
	beforePos, ok := w.Positions.GetPosition(drain)
	if !ok {
		t.Fatal("drain has no position")
	}

	drains.updateDrainMovement()

	after, _ := w.Components.Kinetic.GetComponent(drain)
	if after.PreciseX == before.PreciseX && after.PreciseY == before.PreciseY {
		t.Fatal("drain did not integrate its knockback while kinetic immunity was active")
	}
	afterPos, _ := w.Positions.GetPosition(drain)
	if afterPos == beforePos {
		t.Fatalf("drain remained in cell (%d, %d) after knockback", beforePos.X, beforePos.Y)
	}
}

func TestDrainIntegratesShieldKnockbackDuringKineticImmunity(t *testing.T) {
	w, cursor, drain, drains, combat := testDrainInteractionWorld(t, 10, 10, 10, 10)

	shield := NewShieldSystem(w).(*ShieldSystem)
	shield.HandleEvent(event.GameEvent{
		Type:    event.EventShieldActivate,
		Payload: &event.ShieldActivatePayload{Entity: cursor},
	})
	drains.handleDrainInteractions()
	applyQueuedCombatArea(t, w, combat)

	assertDrainKnockbackIntegrated(t, w, drain, drains)
}

func TestDrainIntegratesExplosionKnockbackDuringKineticImmunity(t *testing.T) {
	w, cursor, drain, drains, combat := testDrainInteractionWorld(t, 5, 5, 8, 5)

	explosion := NewExplosionSystem(w).(*ExplosionSystem)
	explosion.HandleEvent(event.GameEvent{
		Type: event.EventExplosionRequest,
		Payload: &event.ExplosionRequestPayload{
			Entity: cursor,
			X:      5,
			Y:      5,
			Radius: 4,
			Type:   event.ExplosionTypeDust,
		},
	})
	applyQueuedCombatArea(t, w, combat)

	assertDrainKnockbackIntegrated(t, w, drain, drains)
}

func TestDrainStunStillStopsKineticIntegration(t *testing.T) {
	w, _, drain, drains, _ := testDrainInteractionWorld(t, 5, 5, 10, 10)

	kinetic, _ := w.Components.Kinetic.GetPtr(drain)
	kinetic.VelX = 20
	combat, _ := w.Components.Combat.GetPtr(drain)
	combat.StunnedRemaining = time.Second
	combat.RemainingKineticImmunity = parameter.CombatKineticImmunityDuration
	beforeX, beforeY := kinetic.PreciseX, kinetic.PreciseY

	drains.updateDrainMovement()

	if kinetic.PreciseX != beforeX || kinetic.PreciseY != beforeY {
		t.Fatalf("stunned drain moved from (%v, %v) to (%v, %v)",
			beforeX, beforeY, kinetic.PreciseX, kinetic.PreciseY)
	}
}
