package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

func consumeStormBullet(t *testing.T, events []event.GameEvent) *event.BulletSpawnRequestPayload {
	t.Helper()
	for _, ev := range events {
		if ev.Type != event.EventBulletSpawnRequest {
			continue
		}
		if payload, ok := ev.Payload.(*event.BulletSpawnRequestPayload); ok {
			return payload
		}
	}
	t.Fatalf("events = %#v, want EventBulletSpawnRequest", events)
	return nil
}

func TestStormRedBurstRefreshesSharedAim(t *testing.T) {
	w, first, second := testCursorWorld(t)
	storm := NewStormSystem(w).(*StormSystem)

	root := w.CreateEntity(core.DomainShared)
	circle := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(circle, component.PositionComponent{X: 10, Y: 5})
	w.Components.StormCircle.SetComponent(circle, component.StormCircleComponent{
		Pos3D:             vmath.Vec3F{X: 10.5, Y: 5.5, Z: parameter.StormZMid - 1},
		Index:             int(component.StormCircleRed),
		AttackState:       component.StormCircleAttackCooldown,
		CooldownRemaining: 0,
	})
	storm.rootEntity = root
	stormComp := component.StormComponent{}
	stormComp.Circles[component.StormCircleRed] = circle
	stormComp.CirclesAlive[component.StormCircleRed] = true

	// Both cursors begin five cells away. Deterministic roster order chooses slot 0.
	storm.updateCircleAttacks(&stormComp, 50*time.Millisecond)
	circleComp, _ := w.Components.StormCircle.GetPtr(circle)
	if circleComp.AttackState != component.StormCircleAttackActive {
		t.Fatalf("attack state = %v, want active", circleComp.AttackState)
	}
	if circleComp.AttackTargetX != 5 || circleComp.AttackTargetY != 5 {
		t.Fatalf("initial target = (%d, %d), want cursor %d at (5, 5)",
			circleComp.AttackTargetX, circleComp.AttackTargetY, first)
	}
	w.Resources.Event.Queue.Consume()

	// Move the closest cursor to the right while keeping the other farther away.
	// The Shared aim and locally derived bullet turn together.
	w.Positions.SetPosition(first, component.PositionComponent{X: 20, Y: 5})
	w.Positions.SetPosition(second, component.PositionComponent{X: 35, Y: 5})
	storm.updateCircleAttacks(&stormComp, 50*time.Millisecond)
	if circleComp.AttackTargetX != 20 || circleComp.AttackTargetY != 5 {
		t.Fatalf("tracked target = (%d, %d), want (20, 5)",
			circleComp.AttackTargetX, circleComp.AttackTargetY)
	}
	if bullet := consumeStormBullet(t, w.Resources.Event.Queue.Consume()); bullet.VelX <= 0 {
		t.Fatalf("right-facing bullet velocity = (%f, %f), want positive X", bullet.VelX, bullet.VelY)
	}

	// Crossing to the other side turns both the component-driven muzzle and bullet.
	w.Positions.SetPosition(first, component.PositionComponent{X: 0, Y: 5})
	storm.updateCircleAttacks(&stormComp, 50*time.Millisecond)
	if circleComp.AttackTargetX != 0 || circleComp.AttackTargetY != 5 {
		t.Fatalf("tracked coordinates = (%d, %d), want (0, 5)", circleComp.AttackTargetX, circleComp.AttackTargetY)
	}
	if bullet := consumeStormBullet(t, w.Resources.Event.Queue.Consume()); bullet.VelX >= 0 {
		t.Fatalf("left-facing bullet velocity = (%f, %f), want negative X", bullet.VelX, bullet.VelY)
	}

	// A departed cursor leaves the remaining Shared cursor as the target.
	w.DestroyEntity(first)
	storm.updateCircleAttacks(&stormComp, 50*time.Millisecond)
	if circleComp.AttackTargetX != 35 || circleComp.AttackTargetY != 5 {
		t.Fatalf("replacement target = (%d, %d), want cursor %d at (35, 5)",
			circleComp.AttackTargetX, circleComp.AttackTargetY, second)
	}
}
