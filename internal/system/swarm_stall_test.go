package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// newStalledSwarm builds one chasing swarm in a world with no resolvable target,
// which the lock's probe refuses. Its chase is about to expire, so the first
// update tries the lock.
func newStalledSwarm(t *testing.T) (*engine.World, *SwarmSystem, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	s := ready(NewSwarmSystem(w)).(*SwarmSystem)
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
		Machine: aboutToExpire(component.SwarmStateChase, parameter.SwarmChargeInterval),
	})
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Behavior: component.BehaviorSwarm,
		Type:     component.CompositeTypeUnit,
	})

	// Target group 0 is left empty on purpose: resolveBaseTarget answers false,
	// which is exactly what the lock's probe refuses.
	if w.Resources.Target == nil {
		w.Resources.Target = &engine.TargetResource{}
	}
	w.Resources.Target.SetGroup(0, engine.TargetGroupState{})
	return w, s, header
}

// aboutToExpire is an entry of state s whose duration d runs out on the next step
func aboutToExpire(s component.SwarmState, d time.Duration) machine.Instance[component.SwarmState] {
	return machine.Instance[component.SwarmState]{State: s, Elapsed: d - time.Nanosecond, Duration: d}
}

// TestSwarmKeepsIntegratingWhenLockCannotResolve: when no lock target resolves,
// Chase re-arms and keeps homing and integrating. An early return on every tick
// would park the swarm inside a shield that strikes and knocks it back without
// damage, with nothing turning that velocity into movement.
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

// TestAStunnedSwarmStillDies: the hit-point check runs ahead of the step and a
// stunned swarm still counts as active, so a swarm held in stun can neither
// outlive zero hit points nor drop out of swarm.count.
func TestAStunnedSwarmStillDies(t *testing.T) {
	w, s, header := newStalledSwarm(t)

	tick := func() {
		w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
			engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
		s.Update()
	}

	combat, _ := w.Components.Combat.GetPtr(header)
	combat.Status[component.StatusStun].Remaining = parameter.PulseStunDuration

	// A stun suspends movement, not membership: swarm.count and combat.live.swarm
	// disagreeing is what named this defect in the session log.
	tick()
	if n := w.Resources.Status.Ints.Get("swarm.count").Load(); n != 1 {
		t.Fatalf("swarm.count = %d while stunned, want 1", n)
	}

	combat.HitPoints = 0
	w.Resources.Event.Queue.Consume()
	tick()

	killed, destroyed := false, false
	for _, ev := range w.Resources.Event.Queue.Consume() {
		switch ev.Type {
		case event.EventSpeciesKilled:
			killed = true
		case event.EventCompositeDestroyRequest:
			destroyed = true
		}
	}
	if !killed || !destroyed {
		t.Fatalf("stunned swarm at zero hit points: killed=%v destroyed=%v, want both",
			killed, destroyed)
	}
}

// TestAStunIsNotRefreshedWhileItRuns: the window belongs to the target, so a
// later hit — the other participant's included — cannot extend it.
func TestAStunIsNotRefreshedWhileItRuns(t *testing.T) {
	w, _, header := newStalledSwarm(t)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)

	c, _ := w.Components.Combat.GetPtr(header)
	if !combat.applyStunEffect(header, c) {
		t.Fatal("the first stun was refused")
	}
	c.Status[component.StatusStun].Remaining -= parameter.GameUpdateInterval
	held := c.Status[component.StatusStun].Remaining

	if combat.applyStunEffect(header, c) {
		t.Fatal("a second stun was applied while one was running")
	}
	if c.Status[component.StatusStun].Remaining != held {
		t.Fatalf("stun = %v, want %v: a refused stun still moved the timer",
			c.Status[component.StatusStun].Remaining, held)
	}
}

// TestSwarmLeavesLockWhenChargeCannotResolve pins the other half. Lock freezes the
// swarm in place and holds IsEnraged, which is one of the two gates that make the
// shield's ejection a no-op, so a lock that cannot reach charge must not be held.
func TestSwarmLeavesLockWhenChargeCannotResolve(t *testing.T) {
	w, s, header := newStalledSwarm(t)

	// A lock about to expire with no position, which refuses both the charge and
	// the teleport
	w.Components.Swarm.SetComponent(header, component.SwarmComponent{
		Machine: aboutToExpire(component.SwarmStateLock, parameter.SwarmLockDuration),
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
	if sw.Machine.State == component.SwarmStateLock {
		t.Fatal("swarm held in lock with an expired timer: frozen, enraged, and " +
			"immune to the shield that is supposed to eject it")
	}

	combat, _ := w.Components.Combat.GetComponent(header)
	if combat.IsEnraged && sw.Machine.State == component.SwarmStateLock {
		t.Fatal("swarm latched enraged in a state it cannot leave")
	}
}

// TestSwarmLifetimeDespawnsTheWholeComposite: an expired lifetime ends the swarm
// through the composite, as the charge limit does, so its members go with the
// header; a lifetime ending the header alone left them behind. A stunned swarm
// still expires.
func TestSwarmLifetimeDespawnsTheWholeComposite(t *testing.T) {
	w, s, header := newStalledSwarm(t)
	combat, _ := w.Components.Combat.GetPtr(header)
	combat.Status[component.StatusStun].Remaining = time.Second
	timers := ready(NewTimerSystem(w))
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	armTimer(w, header, component.TimerDeadline, parameter.GameUpdateInterval)

	timers.Update()
	s.Update()
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.CompositeDestroyRequestPayload); ok && p.HeaderEntity == header {
			return
		}
	}
	t.Fatal("an expired swarm lifetime did not despawn its composite")
}
