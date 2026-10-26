package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func consumeStormBullet(t *testing.T, events []event.GameEvent) *event.BulletSpawnRequestPayload {
	t.Helper()
	for _, ev := range events {
		if payload, ok := ev.Payload.(*event.BulletSpawnRequestPayload); ok && ev.Type == event.EventBulletSpawnRequest {
			return payload
		}
	}
	t.Fatalf("events = %#v, want EventBulletSpawnRequest", events)
	return nil
}

// openingCircle adds a circle of one colour at (10, 5) and depth z, its first
// cooldown about to run out, and a storm that holds it alone
func openingCircle(storm *StormSystem, kind component.StormCircleType, z float64) (*component.StormComponent, *component.StormCircleComponent) {
	w := storm.world
	circle := storm.createCircleHeader(w.CreateEntity(core.DomainShared), int(kind),
		vmath.Vec3{X: 10.5, Y: 5.5, Z: z}, vmath.Vec3{})
	circleComp, _ := w.Components.StormCircle.GetPtr(circle)
	circleComp.Machine = aboutToExpire(component.StormCircleCooldown, circleVariants[kind].initial)
	stormComp := &component.StormComponent{}
	stormComp.Circles[kind] = circle
	stormComp.CirclesAlive[kind] = true
	w.Resources.Event.Queue.Consume()
	return stormComp, circleComp
}

// redTick is one tick of a red circle's burst: the timers, the storm's step, then
// the mount it armed; it returns what they pushed
func redTick(w *engine.World, storm *StormSystem, mounts, timers engine.System, stormComp *component.StormComponent) []event.GameEvent {
	timers.Update()
	storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
	mounts.Update()
	return w.Resources.Event.Queue.Consume()
}

// TestStormRedTurretTracksTheNearestCursor: the storm arms its red circle's turret
// for the ticks a burst runs, and the mount aims at the nearest cursor by Shared
// positions, ties to the lower slot, so its hostile bullets turn with the target.
func TestStormRedTurretTracksTheNearestCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	storm := ready(NewStormSystem(w)).(*StormSystem)
	mounts := ready(NewMountSystem(w)).(*MountSystem)
	timers := ready(NewTimerSystem(w))
	stormComp, circleComp := openingCircle(storm, component.StormCircleRed, parameter.StormZMid-1)
	mount, _ := w.Components.Mount.GetPtr(stormComp.Circles[component.StormCircleRed])
	tick := func() []event.GameEvent { return redTick(w, storm, mounts, timers, stormComp) }

	// The cooldown runs out, so the burst opens and fires this tick; both cursors are
	// five cells away
	if bullet := consumeStormBullet(t, tick()); circleComp.Machine.State != component.StormCircleActive || bullet.VelX >= 0 {
		t.Fatalf("state = %v bullet = %#v, want an active burst firing left", circleComp.Machine.State, bullet)
	}
	if mount.AimX != 5 || mount.AimY != 5 {
		t.Fatalf("aim = (%d, %d), want cursor %d at (5, 5)", mount.AimX, mount.AimY, first)
	}

	w.Positions.SetPosition(first, component.PositionComponent{X: 20, Y: 5})
	w.Positions.SetPosition(second, component.PositionComponent{X: 35, Y: 5})
	if bullet := consumeStormBullet(t, tick()); !bullet.Hostile || bullet.VelX <= 0 || mount.AimX != 20 {
		t.Fatalf("bullet = %#v aim x = %d, want a hostile right-facing bullet at x 20", bullet, mount.AimX)
	}

	w.Positions.SetPosition(first, component.PositionComponent{X: 0, Y: 5})
	if bullet := consumeStormBullet(t, tick()); bullet.VelX >= 0 {
		t.Fatalf("left-facing bullet velocity = (%f, %f), want negative X", bullet.VelX, bullet.VelY)
	}

	w.DestroyEntity(first)
	tick()
	if mount.AimX != 35 || mount.AimY != 5 {
		t.Fatalf("replacement aim = (%d, %d), want cursor %d at (35, 5)", mount.AimX, mount.AimY, second)
	}
}

// TestAStunHoldsAnAttackAndItsTurret: a stun suspends the circle's machine, so a
// burst keeps the time it has left and its turret, disarmed, fires nothing until
// the stun ends
func TestAStunHoldsAnAttackAndItsTurret(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	storm := ready(NewStormSystem(w)).(*StormSystem)
	mounts := ready(NewMountSystem(w)).(*MountSystem)
	timers := ready(NewTimerSystem(w))
	stormComp, circleComp := openingCircle(storm, component.StormCircleRed, parameter.StormZMid-1)
	circle := stormComp.Circles[component.StormCircleRed]
	mount, _ := w.Components.Mount.GetPtr(circle)
	combat, _ := w.Components.Combat.GetPtr(circle)
	tick := func() []event.GameEvent { return redTick(w, storm, mounts, timers, stormComp) }

	for range 3 {
		tick()
	}
	held := circleComp.Machine
	combat.Status[component.StatusStun] = component.Status{Remaining: parameter.PulseStunDuration}
	for range 10 {
		if events := tick(); len(events) != 0 || mount.Armed || circleComp.Machine != held {
			t.Fatalf("stunned: events = %#v armed = %v machine = %+v, want nothing fired from %+v", events, mount.Armed, circleComp.Machine, held)
		}
	}

	combat.Status[component.StatusStun] = component.Status{}
	consumeStormBullet(t, tick())
	if !mount.Armed || circleComp.Machine.Elapsed != held.Elapsed+parameter.GameUpdateInterval {
		t.Fatalf("after the stun: armed = %v elapsed = %v, want the burst going on from %v", mount.Armed, circleComp.Machine.Elapsed, held.Elapsed)
	}
}

// TestACircleWithNoCursorKeepsItsOutputsCurrent: with no cursor to attack the
// machine holds, its cooldown frozen, while IsInvulnerable follows the circle's
// depth and its attack on every step
func TestACircleWithNoCursorKeepsItsOutputsCurrent(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	storm := ready(NewStormSystem(w)).(*StormSystem)
	stormComp, circleComp := openingCircle(storm, component.StormCircleGreen, parameter.StormZMid+1)
	held := circleComp.Machine

	for _, z := range []float64{parameter.StormZMid + 1, parameter.StormZMid - 1, parameter.StormZMid + 1} {
		circleComp.Pos3D.Z = z
		storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
		if circleComp.IsInvulnerable != (z >= parameter.StormZMid) || circleComp.Machine != held {
			t.Fatalf("z %.1f: invulnerable = %v machine = %+v, want the depth's answer and %+v", z, circleComp.IsInvulnerable, circleComp.Machine, held)
		}
	}

	// An attack leaves the circle open to hits whatever its depth
	circleComp.Machine = machine.Instance[component.StormCircleState]{State: component.StormCircleActive, Duration: parameter.StormGreenRepeatInterval}
	storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
	if circleComp.IsInvulnerable {
		t.Fatal("a concave circle mid-attack reads invulnerable")
	}
}

// TestAnExpiredCooldownWaitsForTheConvex: a circle attacks only from the convex, so
// a cooldown that runs out while it is concave holds until it turns
func TestAnExpiredCooldownWaitsForTheConvex(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	storm := ready(NewStormSystem(w)).(*StormSystem)
	stormComp, circleComp := openingCircle(storm, component.StormCircleGreen, parameter.StormZMid+1)

	for range 3 {
		storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
		if circleComp.Machine.State != component.StormCircleCooldown || !circleComp.IsInvulnerable {
			t.Fatalf("concave: machine %+v invulnerable %v, want the cooldown held", circleComp.Machine, circleComp.IsInvulnerable)
		}
	}
	circleComp.Pos3D.Z = parameter.StormZMid - 1
	storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
	if circleComp.Machine.State != component.StormCircleActive {
		t.Fatalf("convex: machine %+v, want the attack open", circleComp.Machine)
	}
}

// TestTheBlueCueBeamsOnceAnAttack: the blue finds its landing as its attack opens and
// beams a swarm onto it at 800 ‰ of the attack, once an attack however many ticks
// follow; an attack that found no landing beams nothing
func TestTheBlueCueBeamsOnceAnAttack(t *testing.T) {
	beams := func(walled bool) []int {
		w, _, _ := testCursorWorld(t)
		if walled {
			for y := range 24 {
				for x := range 40 {
					spawnWall(w, x, y)
				}
			}
		}
		storm := ready(NewStormSystem(w)).(*StormSystem)
		stormComp, _ := openingCircle(storm, component.StormCircleBlue, parameter.StormZMid-1)
		var ticks []int
		for tick := range 250 {
			storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
			for _, ev := range w.Resources.Event.Queue.Consume() {
				if ev.Type == event.EventMaterializeAreaRequest {
					ticks = append(ticks, tick)
				}
			}
		}
		return ticks
	}

	// Attacks open on ticks 0 and 150 (2.5 s, then the 5 s cooldown); each cue is
	// 2 s in
	if got := beams(false); !slices.Equal(got, []int{40, 190}) {
		t.Fatalf("beams on ticks %v, want 40 and 190", got)
	}
	if got := beams(true); len(got) != 0 {
		t.Fatalf("beams on ticks %v with no landing anywhere, want none", got)
	}
}

// TestStormDrawsBeforeItReadsLivePositions is D-8 for the storm's conditional draws.
// Spawn placement sits behind a wall search that abandons the whole storm, and a
// blue attack's landing behind one that may find no room, so a draw behind either
// would leave the stream at a different position on each instance.
func TestStormDrawsBeforeItReadsLivePositions(t *testing.T) {
	draws := func(blocked bool) [2]uint64 {
		w, _, _ := testCursorWorld(t)
		storm := ready(NewStormSystem(w)).(*StormSystem)
		if blocked {
			for y := range 24 {
				for x := range 40 {
					spawnWall(w, x, y)
				}
			}
		}
		storm.spawnStorm()
		spawned := storm.rng.State()
		stormComp, _ := openingCircle(storm, component.StormCircleBlue, parameter.StormZMid-1)
		storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
		return [2]uint64{spawned, storm.rng.State()}
	}
	if open, walled := draws(false), draws(true); open != walled {
		t.Fatalf("streams after the spawn and the landing = %#x with room, %#x with none", open, walled)
	}
}

// TestTheBlueSwarmLandsAsItsBeamEnds: the swarm a blue attack beams in rides its
// beam, a Shared entity naming the species, and stands on the landing the tick the
// beam, and so its visual, ends: the attack opens on tick 0, the beam on 40 (2 s).
// The storm lands it alone; the fuse crosses only a fusion's.
func TestTheBlueSwarmLandsAsItsBeamEnds(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	storm := ready(NewStormSystem(w)).(*StormSystem)
	b := newBeamWorld(w, storm, ready(NewSwarmSystem(w)), ready(NewFuseSystem(w)))
	stormComp, circle := openingCircle(storm, component.StormCircleBlue, parameter.StormZMid-1)

	for tick := range 40 + beamTicks() + 1 {
		storm.updateCircleAttacks(stormComp, parameter.GameUpdateInterval)
		b.tick()
		beams, swarms := w.Components.Materialize.Entities(), w.Components.Swarm.Entities()
		switch {
		case tick < 40:
			if len(beams) != 0 || len(swarms) != 0 {
				t.Fatalf("tick %d: %d beams and %d swarms before the cue", tick, len(beams), len(swarms))
			}
		case tick < 40+beamTicks():
			if len(beams) != 1 || len(swarms) != 0 {
				t.Fatalf("tick %d: %d beams and %d swarms while the beam flies, want the beam alone", tick, len(beams), len(swarms))
			}
			if beam, _ := w.Components.Materialize.GetComponent(beams[0]); beams[0].Domain() != core.DomainShared || beam.Species != component.SpeciesSwarm {
				t.Fatalf("tick %d: the beam is %v naming %d, want a Shared one naming the swarm", tick, beams[0], beam.Species)
			}
		default:
			requests := b.sent(event.EventSwarmSpawnRequest, 0)
			if len(beams) != 0 || len(swarms) != 1 || len(requests) != 1 || requests[0].Domain != core.DomainShared {
				t.Fatalf("tick %d: %d beams, %d swarms and requests %+v as the beam ended, want one Shared request and its swarm", tick, len(beams), len(swarms), requests)
			}
			if pos, _ := w.Positions.GetPosition(swarms[0]); pos.X != circle.AttackTargetX || pos.Y != circle.AttackTargetY {
				t.Fatalf("the swarm stands at (%d, %d), want the landing (%d, %d)", pos.X, pos.Y, circle.AttackTargetX, circle.AttackTargetY)
			}
		}
	}
}

// TestAStormsEndEndsItsBeams: a beam in flight is a swarm the storm has yet to
// land, so the storm's end ends it and nothing lands after. A fusion's
// player-domain beam and a Shared one naming no species are not the storm's: both
// fly on, and neither lands a swarm.
func TestAStormsEndEndsItsBeams(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	storm := ready(NewStormSystem(w)).(*StormSystem)
	b := newBeamWorld(w, storm, ready(NewSwarmSystem(w)))
	storm.spawnStorm()
	if storm.rootEntity == 0 {
		t.Fatal("no storm spawned")
	}
	storm.beamIn(&circleCtx{circle: &component.StormCircleComponent{LandingFound: true, AttackTargetX: 20, AttackTargetY: 12}})
	others := []event.GameEvent{
		{Type: event.EventMaterializeAreaRequest, Domain: core.DomainPlayer, Payload: &event.MaterializeAreaRequestPayload{
			X: 4, Y: 16, AreaWidth: parameter.SwarmWidth, AreaHeight: parameter.SwarmHeight,
			Type: component.SpawnTypeSwarm, Species: component.SpeciesSwarm}},
		{Type: event.EventMaterializeAreaRequest, Domain: core.DomainShared, Payload: &event.MaterializeAreaRequestPayload{
			X: 30, Y: 16, AreaWidth: parameter.SwarmWidth, AreaHeight: parameter.SwarmHeight, Type: component.SpawnTypeSwarm}},
	}
	b.deliver(append(w.Resources.Event.Queue.Consume(), others...)...)
	b.tick()

	b.deliver(event.GameEvent{Type: event.EventStormCancelRequest})
	if n := w.Components.Materialize.CountEntities(); n != len(others) {
		t.Fatalf("%d beams after the storm's end, want the %d not its own", n, len(others))
	}
	for range beamTicks() {
		b.tick()
	}
	if n := w.Components.Swarm.CountEntities(); n != 0 {
		t.Fatalf("%d swarms landed after the storm ended", n)
	}
}
