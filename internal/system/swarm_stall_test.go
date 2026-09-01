package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// newStalledSwarm builds one chasing swarm in a world with no resolvable target,
// which is the condition enterLockState refuses on. The swarm's charge interval
// is set to expire on the next tick, so the very first update takes the branch
// that used to wedge it.
func newStalledSwarm(t *testing.T) (*engine.World, *SwarmSystem, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	s := NewSwarmSystem(w).(*SwarmSystem)
	s.enabled = true

	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 20, Y: 10})
	w.Components.Kinetic.SetComponent(header, component.KineticComponent{
		Kinetic: physics.Kinetic{PreciseX: 20.5, PreciseY: 10.5},
	})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		HitPoints:        100,
		CombatEntityType: component.CombatEntitySwarm,
	})
	w.Components.Swarm.SetComponent(header, component.SwarmComponent{
		State:                   component.SwarmStateChase,
		ChargeIntervalRemaining: time.Nanosecond,
	})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Behavior: component.BehaviorSwarm,
		Type:     component.CompositeTypeUnit,
	})

	// Target group 0 is left empty on purpose: resolveBaseTarget answers false,
	// which is exactly what enterLockState cannot complete on.
	if w.Resources.Target == nil {
		w.Resources.Target = &engine.TargetResource{}
	}
	w.Resources.Target.SetGroup(0, engine.TargetGroupState{})
	return w, s, header
}

// TestSwarmKeepsIntegratingWhenLockCannotResolve is the regression for the swarm
// found parked inside a player's shield on 2026-08-31.
//
// updateChaseState decremented ChargeIntervalRemaining, called enterLockState and
// returned. enterLockState refuses when no target resolves, and refused without
// re-arming the interval — so every following tick took the same branch and
// returned before applying homing or integrating. The swarm stopped moving
// permanently. The shield kept striking it (shield.shield_hit climbed by 64 over
// 200 ticks in the incident) and combat kept applying knockback impulses to its
// velocity, but with nothing integrating that velocity the swarm never left. The
// shield deals no damage by design, so nothing else resolved it.
func TestSwarmKeepsIntegratingWhenLockCannotResolve(t *testing.T) {
	w, s, header := newStalledSwarm(t)

	// A knockback of the kind the shield applies: velocity only. Integration is
	// what has to turn it into movement.
	kin, _ := w.Components.Kinetic.GetPtr(header)
	kin.VelX, kin.VelY = 12, 0

	start, _ := w.Positions.GetPosition(header)
	for range 20 {
		w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
			engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
		s.Update()
	}

	end, ok := w.Positions.GetPosition(header)
	if !ok {
		t.Fatal("swarm lost its position")
	}
	if end.X == start.X && end.Y == start.Y {
		t.Fatalf("swarm never moved from (%d,%d) in 20 ticks while carrying velocity; "+
			"a failed lock entry froze its integrator", start.X, start.Y)
	}

	if stalls := w.Resources.Status.Ints.Get("swarm.transition_stalls").Load(); stalls == 0 {
		t.Fatal("a refused lock entry was not counted; the condition is invisible in a session log")
	}
}

// TestSwarmLeavesLockWhenChargeCannotResolve pins the other half. Lock freezes the
// swarm in place and holds IsEnraged, which is one of the two gates that make the
// shield's ejection a no-op, so a lock that cannot reach charge must not be held.
func TestSwarmLeavesLockWhenChargeCannotResolve(t *testing.T) {
	w, s, header := newStalledSwarm(t)

	// Lock with an expired timer and no position, which is what enterChargeState
	// refuses on.
	w.Components.Swarm.SetComponent(header, component.SwarmComponent{
		State:         component.SwarmStateLock,
		LockRemaining: time.Nanosecond,
	})
	w.Positions.RemoveEntity(header, false)

	for range 10 {
		w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
			engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
		s.Update()
	}

	sw, ok := w.Components.Swarm.GetComponent(header)
	if !ok {
		t.Fatal("swarm component vanished")
	}
	if sw.State == component.SwarmStateLock {
		t.Fatal("swarm held in lock with an expired timer: frozen, enraged, and " +
			"immune to the shield that is supposed to eject it")
	}

	combat, _ := w.Components.Combat.GetComponent(header)
	if combat.IsEnraged && sw.State == component.SwarmStateLock {
		t.Fatal("swarm latched enraged in a state it cannot leave")
	}
}
