package system

import (
	"maps"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
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

// TestMountShotDrawsDependOnlyOnTickAndHost is D-8 for mounts: a correction can leave
// two instances holding one store in different orders, so a shot's spread is seeded
// from the tick and its host rather than drawn from a stream.
func TestMountShotDrawsDependOnlyOnTickAndHost(t *testing.T) {
	type velocity struct{ x, y float64 }
	volley := func(reversed bool) map[core.Entity]velocity {
		w, _, _ := testCursorWorld(t)
		mounts := NewMountSystem(w).(*MountSystem)
		hosts := []core.Entity{w.CreateEntity(core.DomainShared), w.CreateEntity(core.DomainShared)}
		for i, host := range hosts {
			w.Positions.SetPosition(host, component.PositionComponent{X: 20 + 10*i, Y: 12})
		}
		if reversed {
			hosts[0], hosts[1] = hosts[1], hosts[0]
		}
		for _, host := range hosts {
			w.Components.Mount.SetComponent(host, component.MountComponent{
				Weapon: component.WeaponTurret, Interval: parameter.GameUpdateInterval,
			})
		}
		mounts.Update()
		shots := make(map[core.Entity]velocity)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.BulletSpawnRequestPayload); ok {
				shots[p.Owner] = velocity{p.VelX, p.VelY}
			}
		}
		return shots
	}
	if inOrder, reversed := volley(false), volley(true); len(inOrder) != 2 || !maps.Equal(inOrder, reversed) {
		t.Fatalf("shots = %v in store order, %v reversed; want two identical volleys", inOrder, reversed)
	}
}

// TestPlayerBulletCrossesOnlyItsHit: a cursor's bullet is this instance's alone. A hit
// on a Shared target is one direct request stamped Shared; a hit on a drain stays local.
func TestPlayerBulletCrossesOnlyItsHit(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	bullets := NewBulletSystem(w).(*BulletSystem)

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 9, Y: 3})
	w.Positions.SetPosition(member, component.PositionComponent{X: 9, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeUnit,
		MemberEntries: []component.MemberEntry{{Entity: member, OffsetY: 2}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm})

	drain := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(drain, component.PositionComponent{X: 5, Y: 9})
	w.Components.Combat.SetComponent(drain, component.CombatComponent{CombatEntityType: component.CombatEntityDrain})

	shoot := func(velX, velY float64) event.GameEvent {
		bullets.HandleEvent(event.GameEvent{Type: event.EventBulletSpawnRequest, Payload: &event.BulletSpawnRequestPayload{
			OriginX: 5.5, OriginY: 5.5, VelX: velX, VelY: velY, Owner: cursor,
			MaxLifetime: parameter.TurretBulletLifetime, Attack: component.CombatAttackBullet,
		}})
		bullets.Update()
		events := w.Resources.Event.Queue.Consume()
		if len(events) != 1 || events[0].Type != event.EventCombatAttackDirectRequest {
			t.Fatalf("bullet events = %#v, want one direct request", events)
		}
		return events[0]
	}

	shared := shoot(100, 0)
	hit, _ := shared.Payload.(*event.CombatAttackDirectRequestPayload)
	if shared.Domain != core.DomainShared || hit.TargetEntity != header || hit.HitEntity != member || hit.OwnerEntity != cursor {
		t.Fatalf("shared hit = %#v stamped %v, want header %d through member %d", hit, shared.Domain, header, member)
	}
	local := shoot(0, 100)
	if hit, _ := local.Payload.(*event.CombatAttackDirectRequestPayload); local.Domain != core.DomainPlayer || hit.TargetEntity != drain {
		t.Fatalf("drain hit = %#v stamped %v, want a local hit on %d", hit, local.Domain, drain)
	}
}

// TestPlayerBeamRunsThroughItsOrbAndCrossesOnlyItsImpacts: a cursor's beam runs
// from the cursor through its orb, one cell wide to the orb and wider past it, and
// follows the orb. A Shared target crosses once as its members, owner and charge
// scale; a drain's hit stays local; nothing else leaves this instance.
func TestPlayerBeamRunsThroughItsOrbAndCrossesOnlyItsImpacts(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)

	header := w.CreateEntity(core.DomainShared)
	near := w.CreateEntity(core.DomainShared)
	far := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 9, Y: 2})
	w.Positions.SetPosition(near, component.PositionComponent{X: 7, Y: 5})
	w.Positions.SetPosition(far, component.PositionComponent{X: 12, Y: 6})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type:          component.CompositeTypeUnit,
		MemberEntries: []component.MemberEntry{{Entity: near, OffsetX: -2, OffsetY: 3}, {Entity: far, OffsetX: 3, OffsetY: 4}},
	})
	w.Components.Member.SetComponent(near, component.MemberComponent{HeaderEntity: header})
	w.Components.Member.SetComponent(far, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm})

	drain := func(x, y int) core.Entity {
		e := w.CreateEntity(core.DomainPlayer)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntityDrain})
		return e
	}
	wide, beside := drain(14, 4), drain(7, 4)

	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	weaponComp.Charges[component.WeaponBeam] = 2
	weapon.Update()
	orbs := weapon.orbsOf(cursor)
	w.Positions.SetPosition(orbs[component.WeaponBeam], component.PositionComponent{X: 9, Y: 5})
	w.Resources.Event.Queue.Consume()
	weapon.fireAllWeapons(cursor, weaponComp, orbs)
	weapon.advanceBeams(cursor, orbs, parameter.GameUpdateInterval)

	var crossings int
	struck := make(map[core.Entity]bool)
	for _, ev := range w.Resources.Event.Queue.Consume() {
		p, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload)
		if !ok {
			t.Fatalf("unexpected %s", event.GetEventName(ev.Type))
		}
		if event.OnWire(ev) {
			crossings++
			if p.TargetEntity != header || len(p.HitEntities) != 2 || p.OwnerEntity != cursor || p.Scale != 2 {
				t.Fatalf("crossing = %#v, want header %d with both members at scale 2", p, header)
			}
		}
		struck[p.TargetEntity] = true
	}
	if crossings != 1 || !struck[wide] || struck[beside] {
		t.Fatalf("crossings = %d, struck = %v; want one crossing, %d hit past the orb, %d beside the narrow part spared",
			crossings, struck, wide, beside)
	}

	w.Positions.SetPosition(orbs[component.WeaponBeam], component.PositionComponent{X: 5, Y: 9})
	weapon.advanceBeams(cursor, orbs, parameter.GameUpdateInterval)
	w.Resources.Event.Queue.Consume()
	if beam, _ := w.Components.Beam.GetComponent(orbs[component.WeaponBeam]); beam.Ray.DX != 0 || beam.Ray.DY <= 0 {
		t.Fatalf("ray = %+v after the orb moved south, want it to follow", beam.Ray)
	}
}

// TestMountedBeamWarnsBeforeItStrikes: a laned beam holds its warning for the whole
// warning window without a hit, then strikes the local cursors inside its band.
func TestMountedBeamWarnsBeforeItStrikes(t *testing.T) {
	w, _, second := testCursorWorld(t)
	spawnRemoteCursor(t, w, 2, 25, 5, 9)
	mount := NewMountSystem(w).(*MountSystem)

	host := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(host, component.PositionComponent{X: 10, Y: 5})
	mount.HandleEvent(event.GameEvent{Type: event.EventMountRequest, Payload: &event.MountRequestPayload{
		Host: host, Weapon: component.WeaponBeam, Lane: 1,
	}})

	warningTicks := int(parameter.BeamWarning / parameter.GameUpdateInterval)
	for range warningTicks {
		mount.Update()
		if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
			t.Fatalf("warning tick = %#v, want nothing", events)
		}
	}
	if beam, ok := w.Components.Beam.GetComponent(host); !ok || beam.Phase != component.BeamWarning {
		t.Fatalf("beam = %+v, want the host warning", beam)
	}
	mount.Update()
	events := w.Resources.Event.Queue.Consume()
	heat, ok := events[0].Payload.(*event.HeatAddRequestPayload)
	if len(events) != 1 || !ok || heat.Entity != second {
		t.Fatalf("strike = %#v, want one heat hit on %d, the only local cursor in the band", events, second)
	}
}
