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

	beginDrainTick(t, drains)
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
	beginDrainTick(t, drains)
	drains.handleDrainInteractions()
	applyQueuedCombatArea(t, w, combat)

	assertDrainKnockbackIntegrated(t, w, drain, drains)
}

func TestDrainIntegratesExplosionKnockbackDuringKineticImmunity(t *testing.T) {
	w, cursor, drain, drains, combat := testDrainInteractionWorld(t, 5, 5, 8, 5)

	// Player-domain half of a blast: the producer strikes its own domain before
	// the shared explosion request crosses, so no shared system sees the drain
	var area blastArea
	area.reset([]event.ExplosionCenterEntry{{X: 5, Y: 5}}, 4)
	strikePlayerTargets(w, cursor, &area, component.CombatAttackExplosion)
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

	beginDrainTick(t, drains)
	drains.updateDrainMovement()

	if kinetic.PreciseX != beforeX || kinetic.PreciseY != beforeY {
		t.Fatalf("stunned drain moved from (%v, %v) to (%v, %v)",
			beforeX, beforeY, kinetic.PreciseX, kinetic.PreciseY)
	}
}

func TestDrainCollisionHealsSharedSpeciesThroughCombatEvent(t *testing.T) {
	tests := []struct {
		name       string
		combatType component.CombatEntityType
		setSpecies func(*engine.World, core.Entity)
	}{
		{
			name:       "swarm",
			combatType: component.CombatEntitySwarm,
			setSpecies: func(w *engine.World, entity core.Entity) {
				w.Components.Swarm.SetComponent(entity, component.SwarmComponent{})
			},
		},
		{
			name:       "quasar",
			combatType: component.CombatEntityQuasar,
			setSpecies: func(w *engine.World, entity core.Entity) {
				w.Components.Quasar.SetComponent(entity, component.QuasarComponent{})
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, _, drain, drains, combat := testDrainInteractionWorld(t, 2, 2, 10, 10)
			drainCombat, _ := w.Components.Combat.GetPtr(drain)
			drainCombat.HitPoints = 7

			header := w.CreateEntity(core.DomainShared)
			member := w.CreateEntity(core.DomainShared)
			tc.setSpecies(w, header)
			w.Components.Header.SetComponent(header, component.HeaderComponent{
				Type: component.CompositeTypeUnit,
				MemberEntries: []component.MemberEntry{
					{Entity: member},
				},
			})
			w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
			w.Components.Protection.SetComponent(member, component.ProtectionComponent{
				Mask: component.ProtectFromSpecies,
			})
			w.Components.Combat.SetComponent(header, component.CombatComponent{
				OwnerEntity:      header,
				CombatEntityType: tc.combatType,
				HitPoints:        11,
			})
			w.Positions.SetPosition(header, component.PositionComponent{X: 12, Y: 10})
			w.Positions.SetPosition(member, component.PositionComponent{X: 10, Y: 10})

			beginDrainTick(t, drains)
			drains.handleEntityCollisions()

			healCount := 0
			deathCount := 0
			for _, ev := range w.Resources.Event.Queue.Consume() {
				switch ev.Type {
				case event.EventCombatHealRequest:
					payload, ok := ev.Payload.(*event.CombatHealRequestPayload)
					if !ok {
						t.Fatalf("heal payload = %T, want *event.CombatHealRequestPayload", ev.Payload)
					}
					if payload.TargetEntity != header || payload.Amount != 7 {
						t.Fatalf("heal payload = %+v, want target %d amount 7", payload, header)
					}
					combat.HandleEvent(ev)
					healCount++

				case event.EventDeathBatch:
					payload, ok := ev.Payload.(*event.DeathRequestPayload)
					if !ok {
						t.Fatalf("death payload = %T, want *event.DeathRequestPayload", ev.Payload)
					}
					if len(payload.Entities) != 1 || payload.Entities[0] != drain || payload.EffectEvent != event.EventNone {
						t.Fatalf("death payload = %+v, want silent death of drain %d", payload, drain)
					}
					event.ReleaseDeathRequest(payload)
					deathCount++

				case event.EventSpeciesKilled:
					t.Fatal("consumed drain must not emit a species kill event")
				}
			}

			if healCount != 1 || deathCount != 1 {
				t.Fatalf("events: heal=%d death=%d, want 1 each", healCount, deathCount)
			}
			if got, _ := w.Components.Combat.GetComponent(header); got.HitPoints != 18 {
				t.Fatalf("%s HP = %d, want 18", tc.name, got.HitPoints)
			}
			if !drains.drainCache[0].dying {
				t.Fatal("consumed drain was not marked dying")
			}
		})
	}
}

// beginDrainTick rebuilds the per-tick cache the movement and interaction passes
// iterate; Update does this at the top of every tick. The emptiness check keeps a
// missing cache from turning a negative assertion into a vacuous pass.
func beginDrainTick(t *testing.T, drains *DrainSystem) {
	t.Helper()

	drains.cacheDrainData()
	if len(drains.drainCache) == 0 {
		t.Fatal("drain cache is empty; movement and interaction passes would no-op")
	}
}
