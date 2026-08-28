package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/profile"
)

// knockTarget builds one kinetic combat entity in the given domain
func knockTarget(w *engine.World, d core.Domain, x, y int) core.Entity {
	e := w.CreateEntity(d)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Kinetic.SetComponent(e, component.KineticComponent{})
	w.Components.Combat.SetComponent(e, component.CombatComponent{
		OwnerEntity: e, CombatEntityType: component.CombatEntityDrain, HitPoints: 100,
	})
	return e
}

// TestCombatKnockbackDrawsFromTheTargetsStream asserts D-8: a player-target impulse
// leaves the shared sequence untouched, and the shared case proves that is not vacuous.
func TestCombatKnockbackDrawsFromTheTargetsStream(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	s := NewCombatSystem(w).(*CombatSystem)

	player := knockTarget(w, core.DomainPlayer, 20, 6)
	shared := knockTarget(w, core.DomainShared, 22, 6)

	beforeShared, beforePlayer := s.rngShared.State(), s.rngPlayer.State()
	if !s.applyCollision(1, 0, player, player, &profile.SoftSwarmToSwarm) {
		t.Fatal("player knockback did not apply")
	}
	if got := s.rngShared.State(); got != beforeShared {
		t.Fatalf("player knockback advanced the shared stream: %x -> %x", beforeShared, got)
	}
	if s.rngPlayer.State() == beforePlayer {
		t.Fatal("player knockback drew nothing; this profile's impulse is not randomized")
	}

	beforePlayer = s.rngPlayer.State()
	if !s.applyCollision(1, 0, shared, shared, &profile.SoftSwarmToSwarm) {
		t.Fatal("shared knockback did not apply")
	}
	if s.rngShared.State() == beforeShared {
		t.Fatal("shared knockback drew nothing; the assertion above proves nothing")
	}
	if s.rngPlayer.State() != beforePlayer {
		t.Fatal("shared knockback advanced the player stream")
	}
}

// TestSoftCollisionImpulseDrawsFromTheTargetsStream is the same assertion on the
// other D-8 dual system; tryApplyCollision takes the rule directly, so no profile
// lookup runs between the call and the draw.
func TestSoftCollisionImpulseDrawsFromTheTargetsStream(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)

	rule := s.matrix[component.SpeciesSwarm][component.SpeciesSwarm]
	if rule == nil {
		t.Fatal("swarm-to-swarm collision rule is absent")
	}
	player := knockTarget(w, core.DomainPlayer, 10, 10)
	shared := knockTarget(w, core.DomainShared, 14, 10)

	beforeShared, beforePlayer := s.rngShared.State(), s.rngPlayer.State()
	hits := s.statCollisions.Load()
	s.tryApplyCollision(11, 10, player, rule)
	if s.statCollisions.Load() == hits {
		t.Fatal("player impulse did not land; the source is outside the collision ellipse")
	}
	if got := s.rngShared.State(); got != beforeShared {
		t.Fatalf("player impulse advanced the shared stream: %x -> %x", beforeShared, got)
	}
	if s.rngPlayer.State() == beforePlayer {
		t.Fatal("player impulse drew nothing; this profile is not randomized")
	}

	beforePlayer = s.rngPlayer.State()
	s.tryApplyCollision(15, 10, shared, rule)
	if s.rngShared.State() == beforeShared {
		t.Fatal("shared impulse drew nothing; the assertion above proves nothing")
	}
	if s.rngPlayer.State() != beforePlayer {
		t.Fatal("shared impulse advanced the player stream")
	}
}
