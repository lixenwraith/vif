package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestDisruptorCrossesGeometry keeps player combat local and lets the shared
// explosion consumer derive its own targets from one replicated pulse artifact.
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

	weaponComp, _ := w.Components.Weapon.GetPtr(cursor)
	cursorPos, _ := w.Positions.GetPosition(cursor)
	weapon.fireDisruptorWeapon(cursor, cursorPos, weaponComp, orbSlots{})

	events := w.Resources.Event.Queue.Consume()
	if len(events) != 2 {
		t.Fatalf("disruptor events = %#v, want one player attack and one crossing", events)
	}
	local, ok := events[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || events[0].Type != event.EventCombatAttackAreaRequest || local.TargetEntity != drain {
		t.Fatalf("player event = %#v, want pulse attack on drain %d", events[0], drain)
	}
	crossing, ok := events[1].Payload.(*event.ExplosionRequestPayload)
	if !ok || events[1].Type != event.EventExplosionRequest || events[1].Domain != core.DomainPlayer {
		t.Fatalf("crossing event = %#v, want player-stamped explosion request", events[1])
	}
	if crossing.Entity != cursor || crossing.X != cursorPos.X || crossing.Y != cursorPos.Y ||
		crossing.Radius != parameter.PulseRadiusX || crossing.Attack != component.CombatAttackPulse {
		t.Fatalf("crossing payload = %#v, want complete pulse geometry", crossing)
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
