package system

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
)

// newQuasarWorld builds one homing quasar in the top-left corner of an 80x40 world
// and one cursor six cells to its right, inside the zap circle. Nothing resolves a
// movement target, so the quasar holds its cell and the cursor alone decides its
// state. The spawn's events are consumed.
func newQuasarWorld(t *testing.T) (*engine.World, *QuasarSystem, core.Entity, core.Entity) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	if w.Resources.Target == nil {
		w.Resources.Target = &engine.TargetResource{}
	}

	ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{
		Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{
			X: quasarTestX + 6, Y: quasarTestY, Slot: 0, Control: uint8(component.ControlLocal),
		},
	})
	cursor := w.Resources.Player.Slot(0)
	if cursor == 0 {
		t.Fatal("no cursor in slot 0")
	}

	s := ready(NewQuasarSystem(w)).(*QuasarSystem)
	s.HandleEvent(event.GameEvent{
		Type:    event.EventQuasarSpawnRequest,
		Payload: &event.QuasarSpawnRequestPayload{X: quasarTestX, Y: quasarTestY},
	})
	quasars := w.Components.Quasar.Entities()
	if len(quasars) != 1 {
		t.Fatalf("%d quasars spawned, want one", len(quasars))
	}
	header := quasars[0]
	if q, _ := w.Components.Quasar.GetPtr(header); q.Machine.State != component.QuasarHoming {
		t.Fatalf("spawned in state %d, want homing", q.Machine.State)
	}
	w.Resources.Event.Queue.Consume()
	return w, s, header, cursor
}

const quasarTestX, quasarTestY = 8, 8

// stepQuasars runs one tick of the quasar system and returns the events it raised
func stepQuasars(w *engine.World, s *QuasarSystem) []event.GameEvent {
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	s.Update()
	return w.Resources.Event.Queue.Consume()
}

// moveCursorAway puts the cursor in the far corner, outside the zap circle
func moveCursorAway(w *engine.World, cursor core.Entity) {
	w.Positions.SetPosition(cursor, component.PositionComponent{
		X: w.Resources.Config.MapWidth - 2, Y: w.Resources.Config.MapHeight - 2,
	})
}

func countEvents(events []event.GameEvent, t event.EventType) int {
	n := 0
	for _, ev := range events {
		if ev.Type == t {
			n++
		}
	}
	return n
}

// TestQuasarCursorReturningCancelsTheSplashOnce: leaving the zap circle starts a
// charge whose splash counts down the entry's own duration, and coming back ends
// it through the charge's exit, the one place a charge ends.
func TestQuasarCursorReturningCancelsTheSplashOnce(t *testing.T) {
	w, s, header, cursor := newQuasarWorld(t)
	q, _ := w.Components.Quasar.GetPtr(header)

	moveCursorAway(w, cursor)
	events := stepQuasars(w, s)
	if q.Machine.State != component.QuasarCharging {
		t.Fatalf("cursor out of range: state %d, want charging", q.Machine.State)
	}
	var splash *event.SplashTimerRequestPayload
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.SplashTimerRequestPayload); ok && ev.Type == event.EventSplashTimerRequest {
			splash = p
		}
	}
	if splash == nil || splash.Duration != q.Machine.Duration || splash.Duration != parameter.QuasarChargeDuration {
		t.Fatalf("splash %+v, want one counting down the entry's %v", splash, q.Machine.Duration)
	}
	for range 10 {
		stepQuasars(w, s)
	}

	w.Positions.SetPosition(cursor, component.PositionComponent{X: quasarTestX + 6, Y: quasarTestY})
	events = stepQuasars(w, s)
	if q.Machine.State != component.QuasarHoming {
		t.Fatalf("cursor back in range: state %d, want homing", q.Machine.State)
	}
	if n := countEvents(events, event.EventSplashTimerCancel); n != 1 {
		t.Fatalf("returning cursor cancelled the splash %d times, want once", n)
	}
	if n := countEvents(stepQuasars(w, s), event.EventSplashTimerCancel); n != 0 {
		t.Fatalf("the step after the return cancelled %d more splashes", n)
	}
}

// TestQuasarStunWhileZappingEndsTheZap: combat refuses to stun a shielded quasar,
// and the machine does not rely on it. A stun that lands during a zap despawns the
// lightning and drops the shield in the step that sees it, then freezes the quasar
// at home though the cursor is still out of range.
func TestQuasarStunWhileZappingEndsTheZap(t *testing.T) {
	w, s, header, cursor := newQuasarWorld(t)
	q, _ := w.Components.Quasar.GetPtr(header)
	combat, _ := w.Components.Combat.GetPtr(header)
	shield, _ := w.Components.Shield.GetPtr(header)

	moveCursorAway(w, cursor)
	ticks := int(parameter.QuasarChargeDuration / parameter.GameUpdateInterval)
	for range ticks + 1 {
		stepQuasars(w, s)
	}
	if q.Machine.State != component.QuasarZapping || !q.IsShielded || !shield.Active || !combat.IsEnraged {
		t.Fatalf("after the charge: state %d shielded %v shield %v enraged %v, want a shielded, enraged zap",
			q.Machine.State, q.IsShielded, shield.Active, combat.IsEnraged)
	}

	combat.Status[component.StatusStun] = component.Status{Remaining: parameter.PulseStunDuration}
	events := stepQuasars(w, s)
	if q.Machine.State != component.QuasarHoming || q.IsShielded || shield.Active || combat.IsEnraged {
		t.Fatalf("stunned zap: state %d shielded %v shield %v enraged %v, want homing, unshielded, calm",
			q.Machine.State, q.IsShielded, shield.Active, combat.IsEnraged)
	}
	despawned := false
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.LightningDespawnRequestPayload); ok && p.Owner == header {
			despawned = true
		}
	}
	if !despawned {
		t.Fatalf("stunned zap left its lightning: events %v", events)
	}

	if events := stepQuasars(w, s); q.Machine.State != component.QuasarHoming ||
		countEvents(events, event.EventSplashTimerRequest) != 0 {
		t.Fatalf("a stunned quasar charged: state %d", q.Machine.State)
	}
}

// TestQuasarFirstSpeedIncreaseAfterTwentyMovementTicks: the multiplier counts the
// time the quasar spends moving, from its spawn. It first lifts on the twentieth
// movement tick, and a stun, which stops movement, stops the count with it.
func TestQuasarFirstSpeedIncreaseAfterTwentyMovementTicks(t *testing.T) {
	w, s, header, _ := newQuasarWorld(t)
	q, _ := w.Components.Quasar.GetPtr(header)
	combat, _ := w.Components.Combat.GetPtr(header)

	half := parameter.QuasarSpeedIncreaseTicks / 2
	for range half {
		stepQuasars(w, s)
	}
	combat.Status[component.StatusStun] = component.Status{Remaining: parameter.PulseStunDuration}
	for range 3 * parameter.QuasarSpeedIncreaseTicks {
		stepQuasars(w, s)
	}
	combat.Status[component.StatusStun] = component.Status{}

	for moved := half + 1; moved < parameter.QuasarSpeedIncreaseTicks; moved++ {
		stepQuasars(w, s)
		if q.SpeedMultiplier != 1 {
			t.Fatalf("movement tick %d: multiplier %v, want 1 until tick %d",
				moved, q.SpeedMultiplier, parameter.QuasarSpeedIncreaseTicks)
		}
	}
	stepQuasars(w, s)
	if want := 1 + parameter.QuasarSpeedIncreasePercent; q.SpeedMultiplier != want || q.SpeedElapsed != 0 {
		t.Fatalf("movement tick %d: multiplier %v carrying %v, want %v carrying nothing",
			parameter.QuasarSpeedIncreaseTicks, q.SpeedMultiplier, q.SpeedElapsed, want)
	}
}

type quasarChange struct {
	from, to component.QuasarState
	cause    machine.Cause
}

// TestQuasarChargeWithoutAimFallsBackToHoming: a charge's timeout commits on every
// path. With no cursor to aim at, the zap's probe refuses and the second candidate
// takes the quasar home, ending the splash, in the step the charge expires. Live
// sensing reads no cursor as in range, so the context is set by hand; its standing
// "out of range" then charges again from home in the same step, a fresh entry.
func TestQuasarChargeWithoutAimFallsBackToHoming(t *testing.T) {
	w, s, header, cursor := newQuasarWorld(t)
	var changes []quasarChange
	s.graph = newQuasarGraph(s, func(from, to component.QuasarState, cause machine.Cause) {
		changes = append(changes, quasarChange{from, to, cause})
	})
	q, _ := w.Components.Quasar.GetPtr(header)
	q.Machine = machine.Instance[component.QuasarState]{
		State:    component.QuasarCharging,
		Elapsed:  parameter.QuasarChargeDuration - parameter.GameUpdateInterval,
		Duration: parameter.QuasarChargeDuration,
	}
	ready(NewCursorSystem(w)).(*CursorSystem).despawn(&event.CursorDespawnRequestPayload{Entity: cursor})
	w.Resources.Event.Queue.Consume()

	s.ctx = quasarCtx{entity: header, quasar: q,
		dt: parameter.GameUpdateInterval, dtSec: parameter.GameUpdateInterval.Seconds()}
	s.graph.Step(&q.Machine, &s.ctx, parameter.GameUpdateInterval)
	events := w.Resources.Event.Queue.Consume()

	if len(changes) == 0 || changes[0] != (quasarChange{component.QuasarCharging, component.QuasarHoming, machine.CauseTimeout}) {
		t.Fatalf("changes %v, want the timeout to take the charge home first", changes)
	}
	if n := countEvents(events, event.EventLightningSpawnRequest); n != 0 {
		t.Fatalf("a zap with no cursor spawned %d lightnings", n)
	}
	if n := countEvents(events, event.EventSplashTimerCancel); n != 1 {
		t.Fatalf("the fallen-back charge cancelled its splash %d times, want once", n)
	}
	if q.Machine.Expired() {
		t.Fatalf("instance %+v is still an expired charge", q.Machine)
	}
}

// BenchmarkQuasarUpdate measures QuasarSystem.Update alone, apart from what
// BenchmarkQuasars (internal/app) times in the whole tick: n quasars on a 200x60 map
// hold their cells while the cursor crosses between corners every 100 ticks, so the
// far ones charge, zap and come home. The queue drains each tick inside the timing,
// so the events' own cost counts.
func BenchmarkQuasarUpdate(b *testing.B) {
	for _, n := range []int{1, 16, 64, 256} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 200, 60, engine.NewManualClock())
			if w.Resources.Target == nil {
				w.Resources.Target = &engine.TargetResource{}
			}
			ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{
				Type:    event.EventCursorSpawnRequest,
				Payload: &event.CursorSpawnRequestPayload{X: 2, Y: 2, Slot: 0, Control: uint8(component.ControlLocal)},
			})
			cursor := w.Resources.Player.Slot(0)
			s := ready(NewQuasarSystem(w)).(*QuasarSystem)
			mapW, mapH := w.Resources.Config.MapWidth, w.Resources.Config.MapHeight
			cols := min(n, 32)
			rows := (n + cols - 1) / cols
			for i := range n {
				s.HandleEvent(event.GameEvent{Type: event.EventQuasarSpawnRequest, Payload: &event.QuasarSpawnRequestPayload{
					X: 4 + i%cols*(mapW-8)/max(cols-1, 1),
					Y: 3 + i/cols*(mapH-6)/max(rows-1, 1),
				}})
			}
			if got := w.Components.Quasar.CountEntities(); got != n {
				b.Fatalf("%d quasars spawned, want %d", got, n)
			}
			w.Resources.Event.Queue.Consume()
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				if i%100 == 0 {
					x, y := 2, 2
					if i/100%2 == 1 {
						x, y = mapW-3, mapH-3
					}
					w.Positions.SetPosition(cursor, component.PositionComponent{X: x, Y: y})
				}
				w.Resources.Time.Update(engine.SimTime(uint64(i), parameter.GameUpdateInterval),
					engine.SimTime(uint64(i), parameter.GameUpdateInterval), parameter.GameUpdateInterval)
				s.Update()
				w.Resources.Event.Queue.Consume()
			}
		})
	}
}
