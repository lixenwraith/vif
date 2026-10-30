package system

import (
	"maps"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestDisruptorCrossesGeometry fires a Player-domain pulse that stays on the
// disruptor orb's cell; its strike keeps player combat local and crosses one pulse
// artifact, from which the shared explosion consumer derives its own targets.
func TestDisruptorCrossesGeometry(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)
	pulses := ready(NewPulseSystem(w)).(*PulseSystem)
	explosion := ready(NewExplosionSystem(w)).(*ExplosionSystem)

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
	weaponComp.Charges[component.WeaponGun] = 0 // the pulse's artifacts alone
	weapon.fireLoadout(cursor, weaponComp, orbs)
	if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
		t.Fatalf("fire events = %#v, want the pulse to strike from its own system", events)
	}
	fired := w.Components.Pulse.Entities()
	if len(fired) != 1 || fired[0].Domain() != core.DomainPlayer {
		t.Fatalf("pulses = %v, want one Player-domain pulse", fired)
	}
	if pulse, _ := w.Components.Pulse.GetComponent(fired[0]); pulse.X != 8 || pulse.Y != 7 || pulse.Owner != cursor || pulse.Anchor != 0 {
		t.Fatalf("pulse = %#v, want one staying on the orb's cell, owned by %d", pulse, cursor)
	}

	pulses.Update()
	events := w.Resources.Event.Queue.Consume()
	if len(events) != 2 {
		t.Fatalf("strike events = %#v, want a player attack and a crossing", events)
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
// Its pulse is a Shared entity riding the host, every instance's alike; its hits are
// each instance's own, and a remote cursor's hit is its owner's.
func TestMountedWeaponRaisesOnlyLocalEvents(t *testing.T) {
	w, first, second := testCursorWorld(t)
	remote := spawnRemoteCursor(t, w, 2, 20, 6, 9)
	mount := ready(NewMountSystem(w)).(*MountSystem)
	pulses := ready(NewPulseSystem(w)).(*PulseSystem)

	host := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(host, component.PositionComponent{X: 10, Y: 5})
	mount.HandleEvent(event.GameEvent{Type: event.EventMountRequest,
		Payload: &event.MountRequestPayload{Host: host, Weapon: component.WeaponDisruptor}})
	mount.Update()
	pulses.Update()

	struck := make(map[core.Entity]bool)
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Domain != core.DomainPlayer {
			t.Fatalf("%s stamped %v, want a local event", event.GetEventName(ev.Type), ev.Domain)
		}
		if p, ok := ev.Payload.(*event.HeatAddRequestPayload); ok {
			struck[p.Entity] = true
		} else {
			t.Fatalf("unexpected %s", event.GetEventName(ev.Type))
		}
	}
	if !struck[first] || !struck[second] || struck[remote] {
		t.Fatalf("struck = %v; want both local cursors, not %d", struck, remote)
	}
	live := w.Components.Pulse.Entities()
	if len(live) != 1 || live[0].Domain() != core.DomainShared {
		t.Fatalf("pulses = %v, want one Shared pulse", live)
	}
	if pulse, _ := w.Components.Pulse.GetComponent(live[0]); pulse.Anchor != host || !pulse.Hostile || pulse.Palette != component.PaletteHostile {
		t.Fatalf("pulse = %#v, want a hostile one riding host %d", pulse, host)
	}
}

// TestMountShotDrawsDependOnlyOnTickAndHost is D-8 for mounts: a correction can leave
// two instances holding one store in different orders, so a shot's spread is seeded
// from the tick and its host rather than drawn from a stream.
func TestMountShotDrawsDependOnlyOnTickAndHost(t *testing.T) {
	type velocity struct{ x, y float64 }
	volley := func(reversed bool) map[core.Entity]velocity {
		w, _, _ := testCursorWorld(t)
		mounts := ready(NewMountSystem(w)).(*MountSystem)
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
	bullets := ready(NewBulletSystem(w)).(*BulletSystem)

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

// TestPlayerRayRunsThroughItsOrbAndCrossesOnlyItsImpacts: a cursor's ray runs
// from the cursor through its orb, one cell wide to the orb and wider past it, and
// follows the orb. A Shared target crosses once as its members, owner and charge
// scale; a drain's hit stays local; nothing else leaves this instance.
func TestPlayerRayRunsThroughItsOrbAndCrossesOnlyItsImpacts(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapon := ready(NewWeaponSystem(w)).(*WeaponSystem)

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
	weaponComp.Charges[component.WeaponEmitter] = 2
	weaponComp.Charges[component.WeaponGun] = 0 // the ray's artifacts alone
	weapon.Update()
	orbs := weapon.orbsOf(cursor)
	w.Positions.SetPosition(orbs[component.WeaponEmitter], component.PositionComponent{X: 9, Y: 5})
	w.Resources.Event.Queue.Consume()
	weapon.fireLoadout(cursor, weaponComp, orbs)
	weapon.advanceRays(cursor, orbs, parameter.GameUpdateInterval)

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

	w.Positions.SetPosition(orbs[component.WeaponEmitter], component.PositionComponent{X: 5, Y: 9})
	weapon.advanceRays(cursor, orbs, parameter.GameUpdateInterval)
	w.Resources.Event.Queue.Consume()
	if ray, _ := w.Components.Ray.GetComponent(orbs[component.WeaponEmitter]); ray.Ray.DX != 0 || ray.Ray.DY <= 0 {
		t.Fatalf("ray = %+v after the orb moved south, want it to follow", ray.Ray)
	}
}

// TestMountedRayWarnsBeforeItStrikes: a laned ray holds its warning for the whole
// warning window without a hit, then strikes the local cursors inside its band.
func TestMountedRayWarnsBeforeItStrikes(t *testing.T) {
	w, _, second := testCursorWorld(t)
	spawnRemoteCursor(t, w, 2, 25, 5, 9)
	mount := ready(NewMountSystem(w)).(*MountSystem)

	host := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(host, component.PositionComponent{X: 10, Y: 5})
	mount.HandleEvent(event.GameEvent{Type: event.EventMountRequest, Payload: &event.MountRequestPayload{
		Host: host, Weapon: component.WeaponEmitter, Lane: 1,
	}})

	warningTicks := int(parameter.RayWarning / parameter.GameUpdateInterval)
	for range warningTicks {
		mount.Update()
		if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
			t.Fatalf("warning tick = %#v, want nothing", events)
		}
	}
	if ray, ok := w.Components.Ray.GetComponent(host); !ok || ray.Phase != component.RayWarning {
		t.Fatalf("ray = %+v, want the host warning", ray)
	}
	mount.Update()
	events := w.Resources.Event.Queue.Consume()
	heat, ok := events[0].Payload.(*event.HeatAddRequestPayload)
	if len(events) != 1 || !ok || heat.Entity != second {
		t.Fatalf("strike = %#v, want one heat hit on %d, the only local cursor in the band", events, second)
	}
}
