package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
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
	if !s.applyCollision(event.CrossingID{}, 0, true, 1, 0, player, player, &profile.SoftSwarmToSwarm) {
		t.Fatal("player knockback did not apply")
	}
	if got := s.rngShared.State(); got != beforeShared {
		t.Fatalf("player knockback advanced the shared stream: %x -> %x", beforeShared, got)
	}
	if s.rngPlayer.State() == beforePlayer {
		t.Fatal("player knockback drew nothing; this profile's impulse is not randomized")
	}

	beforePlayer = s.rngPlayer.State()
	if !s.applyCollision(event.CrossingID{}, 0, true, 1, 0, shared, shared, &profile.SoftSwarmToSwarm) {
		t.Fatal("shared knockback did not apply")
	}
	if s.rngShared.State() == beforeShared {
		t.Fatal("shared knockback drew nothing; the assertion above proves nothing")
	}
	if s.rngPlayer.State() != beforePlayer {
		t.Fatal("shared knockback advanced the player stream")
	}
}

// TestCombatKnockbackFollowsTheArtifactNotTheStream is D-3 where D-8 stops. An
// ordinary crossing applies at once on its producer and a playout lead later
// everywhere else, so two instances reach the same impulse only if it is a function
// of the artifact rather than of a stream position they consume at different ticks.
func TestCombatKnockbackFollowsTheArtifactNotTheStream(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	s := NewCombatSystem(w).(*CombatSystem)
	target := knockTarget(w, core.DomainShared, 22, 6)
	id := event.CrossingID{CrossingSource: 2, CrossingSeq: 7}

	before := s.rngShared.State()
	if !s.applyCollision(id, 0, true, 1, 0, target, target, &profile.SoftSwarmToSwarm) {
		t.Fatal("the crossing's knockback did not apply")
	}
	first, _ := w.Components.Kinetic.GetComponent(target)
	if s.rngShared.State() != before {
		t.Fatal("a crossing's knockback consumed the shared stream, which the other instance consumes elsewhere")
	}

	// The other instance reaches this artifact having applied another one first.
	s.rngShared.Next()
	w.Components.Kinetic.SetComponent(target, component.KineticComponent{})
	if !s.applyCollision(id, 0, true, 1, 0, target, target, &profile.SoftSwarmToSwarm) {
		t.Fatal("the second application did not apply")
	}
	if again, _ := w.Components.Kinetic.GetComponent(target); again != first {
		t.Fatalf("the same artifact produced %v and then %v", first.Kinetic, again.Kinetic)
	}
}

// TestSoftCollisionImpulseFollowsTheTickAndThePair is D-8 where a stream stops. A
// collision is conditional on live positions and on a population a crossing thins a
// playout lead apart, so it is not work every instance does at the same tick and
// may not take a shared stream position: one member the producer has already killed
// would cost the two a different number of draws and desync every later impulse.
func TestSoftCollisionImpulseFollowsTheTickAndThePair(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewSoftCollisionSystem(w).(*SoftCollisionSystem)
	s.Init()

	rule := s.matrix[component.SpeciesSwarm][component.SpeciesSwarm]
	if rule == nil {
		t.Fatal("swarm-to-swarm collision rule is absent")
	}
	source := knockTarget(w, core.DomainShared, 15, 10)
	player := knockTarget(w, core.DomainPlayer, 10, 10)
	shared := knockTarget(w, core.DomainShared, 14, 10)

	beforePlayer := s.rngPlayer.State()
	hits := s.statCollisions.Load()
	s.tryApplyCollision(source, 11, 10, player, rule)
	if s.statCollisions.Load() == hits {
		t.Fatal("player impulse did not land; the source is outside the collision ellipse")
	}
	if s.rngPlayer.State() == beforePlayer {
		t.Fatal("player impulse drew nothing; this profile is not randomized")
	}

	beforePlayer = s.rngPlayer.State()
	s.tryApplyCollision(source, 15, 10, shared, rule)
	first, _ := w.Components.Kinetic.GetComponent(shared)
	if s.rngPlayer.State() != beforePlayer {
		t.Fatal("shared impulse advanced the player stream")
	}
	if first.Kinetic == (physics.Kinetic{}) {
		t.Fatal("shared impulse did nothing; the comparison below proves nothing")
	}

	// The other instance still holds a member this one's producer already killed, so
	// it reaches the same pair having resolved a different number of collisions.
	w2 := engine.NewWorld()
	engine.NewGameContextWithClock(w2, 40, 24, engine.NewManualClock())
	s2 := NewSoftCollisionSystem(w2).(*SoftCollisionSystem)
	s2.Init()
	source2 := knockTarget(w2, core.DomainShared, 15, 10)
	shared2 := knockTarget(w2, core.DomainShared, 14, 10)
	doomed := knockTarget(w2, core.DomainShared, 16, 10)
	if source2 != source || shared2 != shared {
		t.Fatalf("shared identity differs between instances: %d/%d", source2, shared2)
	}
	s2.tryApplyCollision(source2, 15, 10, doomed, rule)
	s2.tryApplyCollision(source2, 15, 10, shared2, rule)
	if got, _ := w2.Components.Kinetic.GetComponent(shared2); got != first {
		t.Fatalf("one extra collision moved the pair's impulse: %v here, %v there",
			first.Kinetic, got.Kinetic)
	}
}
