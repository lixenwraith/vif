package system

import (
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
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

// TestStormRedTurretTracksTheNearestCursor: the storm arms its red circle's turret
// for the ticks a burst runs, and the mount aims at the nearest cursor by Shared
// positions, ties to the lower slot, so its hostile bullets turn with the target.
func TestStormRedTurretTracksTheNearestCursor(t *testing.T) {
	w, first, second := testCursorWorld(t)
	storm := NewStormSystem(w).(*StormSystem)
	mounts := NewMountSystem(w).(*MountSystem)

	root := w.CreateEntity(core.DomainShared)
	circle := storm.createCircleHeader(root, int(component.StormCircleRed),
		vmath.Vec3F{X: 10.5, Y: 5.5, Z: parameter.StormZMid - 1}, vmath.Vec3F{})
	circleComp, _ := w.Components.StormCircle.GetPtr(circle)
	circleComp.AttackState = component.StormCircleAttackCooldown
	stormComp := component.StormComponent{}
	stormComp.Circles[component.StormCircleRed] = circle
	stormComp.CirclesAlive[component.StormCircleRed] = true
	mount, _ := w.Components.Mount.GetPtr(circle)
	w.Resources.Event.Queue.Consume()

	tick := func() []event.GameEvent {
		storm.updateCircleAttacks(&stormComp, parameter.GameUpdateInterval)
		mounts.Update()
		return w.Resources.Event.Queue.Consume()
	}

	// The burst opens this tick and fires from the next; both cursors are five cells away
	if events := tick(); circleComp.AttackState != component.StormCircleAttackActive || len(events) != 0 {
		t.Fatalf("state = %v events = %#v, want an active burst that has not fired", circleComp.AttackState, events)
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

// TestStormDrawsBeforeItReadsLivePositions is D-8 for the storm's conditional draw.
// Spawn placement sits behind a wall search that abandons the whole storm, so a draw
// behind it would leave the stream at a different position on each instance.
func TestStormDrawsBeforeItReadsLivePositions(t *testing.T) {
	spawn := func(blocked bool) uint64 {
		w, _, _ := testCursorWorld(t)
		storm := NewStormSystem(w).(*StormSystem)
		if blocked {
			for y := range 24 {
				for x := range 40 {
					spawnWall(w, x, y)
				}
			}
		}
		storm.spawnStorm()
		return storm.rng.State()
	}
	if open, walled := spawn(false), spawn(true); open != walled {
		t.Fatalf("spawn stream = %#x with room, %#x with none", open, walled)
	}
}
