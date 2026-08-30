package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// TestExplosionCombatDoesNotDependOnVisualMergeState deliberately gives one
// instance the nearby center that used to absorb the request. Presentation is
// player-domain state; it must not decide whether shared combat is emitted.
func TestExplosionCombatDoesNotDependOnVisualMergeState(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	explosion := NewExplosionSystem(w).(*ExplosionSystem)

	header := w.CreateEntity(core.DomainShared)
	member := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 7, Y: 5})
	w.Positions.SetPosition(member, component.PositionComponent{X: 7, Y: 5})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Type: component.CompositeTypeUnit, MemberEntries: []component.MemberEntry{{Entity: member}},
	})
	w.Components.Member.SetComponent(member, component.MemberComponent{HeaderEntity: header})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity: header, CombatEntityType: component.CombatEntityStorm, HitPoints: 10,
	})

	// This is a legitimate difference between participants once remote explosion
	// visuals are local. In the old coupled implementation it suppresses combat.
	w.Resources.Transient.ExplosionBacking[0] = engine.ExplosionCenter{
		X: 7, Y: 5, Radius: 4, Intensity: 1, DurNano: 1_000_000_000,
		Type: event.ExplosionTypeMissile,
	}
	w.Resources.Transient.ExplosionCount = 1

	explosion.HandleEvent(event.GameEvent{
		Type: event.EventExplosionRequest,
		Payload: &event.ExplosionRequestPayload{
			Entity: cursor, X: 7, Y: 5, Radius: 4,
			Attack: component.CombatAttackMissile, Type: event.ExplosionTypeMissile,
		},
	})

	events := w.Resources.Event.Queue.Consume()
	if len(events) != 1 || events[0].Type != event.EventCombatAttackAreaRequest {
		t.Fatalf("derived events = %#v, want one shared area attack despite the local visual center", events)
	}
	p, ok := events[0].Payload.(*event.CombatAttackAreaRequestPayload)
	if !ok || p.TargetEntity != header || len(p.HitEntities) != 1 || p.HitEntities[0] != member {
		t.Fatalf("area attack = %#v, want header %d member %d", events[0].Payload, header, member)
	}
}
