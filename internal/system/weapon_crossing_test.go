package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestDisruptorCrossesGeometry keeps player combat and the ring local, centres both
// on the disruptor orb, and lets the shared explosion consumer derive its own
// targets from one replicated pulse artifact.
func TestDisruptorCrossesGeometry(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	explosion := NewExplosionSystem(w).(*ExplosionSystem)

	drain := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(drain, component.PositionComponent{X: 7, Y: 5})
	w.Components.Drain.SetComponent(drain, component.DrainComponent{})
	w.Components.Combat.SetComponent(drain, component.CombatComponent{
		OwnerEntity:      drain,
		CombatEntityType: component.CombatEntityDrain,
		HitPoints:        1,
	})

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 9, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 9, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeUnit,
		MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity:      header,
		CombatEntityType: component.CombatEntitySwarm,
		HitPoints:        1,
	})

	orb := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(orb, component.PositionComponent{X: 8, Y: 7})
	var orbs orbSlots
	orbs[component.WeaponDisruptor] = orb

	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	weaponComp.Charges[component.WeaponDisruptor] = 1
	weapon.fireAllWeapons(cursor, weaponComp, orbs)

	events := w.Resources.Event.Queue.Consume()
	if len(events) != 3 {
		t.Fatalf("disruptor events = %#v, want a player attack, a crossing and a ring", events)
	}
	local, ok := events[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || events[0].Type != event.EventCombatAttackAreaRequest || local.TargetEntity != drain {
		t.Fatalf("player event = %#v, want pulse attack on drain %d", events[0], drain)
	}
	crossing, ok := events[1].Payload.(*event.ExplosionRequestPayload)
	if !ok || events[1].Type != event.EventExplosionRequest || events[1].Domain != core.DomainPlayer {
		t.Fatalf("crossing event = %#v, want player-stamped explosion request", events[1])
	}
	if crossing.Entity != cursor || crossing.X != 8 || crossing.Y != 7 ||
		crossing.Radius != parameter.PulseRadiusX || crossing.Attack != component.CombatAttackPulse {
		t.Fatalf("crossing payload = %#v, want complete pulse geometry at the orb", crossing)
	}
	ring, ok := events[2].Payload.(*event.PulseVisualRequestPayload)
	if !ok || events[2].Domain != core.DomainPlayer || ring.X != 8 || ring.Y != 7 {
		t.Fatalf("ring event = %#v, want a local pulse visual at the orb", events[2])
	}

	explosion.HandleEvent(events[1])
	derived := w.Resources.Event.Queue.Consume()
	if len(derived) != 1 || derived[0].Type != event.EventCombatAttackAreaRequest {
		t.Fatalf("derived events = %#v, want one shared area attack", derived)
	}
	shared, ok := derived[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || shared.TargetEntity != header || len(shared.HitEntities) != 1 || shared.HitEntities[0] != member {
		t.Fatalf("shared attack = %#v, want header %d member %d", derived[0].Payload, header, member)
	}
}

// TestMountedWeaponRaisesOnlyLocalEvents: a Shared host's weapon crosses nothing.
// Its hits and its ring are each instance's own; a remote cursor's hit is its owner's.
func TestMountedWeaponRaisesOnlyLocalEvents(t *testing.T) {
	w, first, second := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 20, 6, 9)
	mount := NewMountSystem(w).(*MountSystem)

	host := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(host, component.PositionComponent{X: 10, Y: 5})
	mount.HandleEvent(event.GameEvent{Type: event.EventMountRequest,
		Payload: &event.MountRequestPayload{Host: host, Weapon: component.WeaponDisruptor}})
	mount.Update()

	struck := make(map[core.Entity]bool)
	rings := 0
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Domain != core.DomainPlayer {
			t.Fatalf("%s stamped %v, want a local event", event.GetEventName(ev.Type), ev.Domain)
		}
		switch p := ev.Payload.(type) {
		case *event.HeatAddRequestPayload:
			struck[p.Entity] = true
		case *event.PulseVisualRequestPayload:
			if p.X != 10 || p.Y != 5 || p.Palette != component.PaletteHostile {
				t.Fatalf("ring = %#v, want a hostile ring at the host", p)
			}
			rings++
		default:
			t.Fatalf("unexpected %s", event.GetEventName(ev.Type))
		}
	}
	if !struck[first] || !struck[second] || struck[remote] || rings != 1 {
		t.Fatalf("struck = %v, rings = %d; want both local cursors, not %d, and one ring", struck, rings, remote)
	}
}
