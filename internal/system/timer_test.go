package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// timerTick opens a tick the way the scheduler does: the death batches the
// previous tick queued settle in pre-dispatch, then the timer ages every slot.
// Other queued events are dropped.
type timerTick struct {
	w      *engine.World
	timer  engine.System
	deaths *DeathSystem
}

func newTimerTick(w *engine.World) *timerTick {
	w.Resources.Time.DeltaTime = parameter.GameUpdateInterval
	return &timerTick{w: w, timer: NewTimerSystem(w), deaths: NewDeathSystem(w).(*DeathSystem)}
}

func (tt *timerTick) open() {
	for _, ev := range tt.w.Resources.Event.Queue.Consume() {
		if ev.Type == event.EventDeathBatch {
			tt.deaths.HandleEvent(ev)
		}
	}
	tt.timer.Update()
}

// lightningEntity is the world's only bolt, zero when there is none
func lightningEntity(w *engine.World) core.Entity {
	if bolts := w.Components.Lightning.Entities(); len(bolts) == 1 {
		return bolts[0]
	}
	return 0
}

// TestLifetimesKeepTheirTickCounts pins two lifetimes against the counts their
// own countdowns produced: a turret bullet moves on 80 ticks (its age had to
// exceed the limit), and a spirit draws 10 frames, the last on its target,
// however soon the death batch settles after its lifetime ends.
func TestLifetimesKeepTheirTickCounts(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	tick := newTimerTick(w)
	bullets := NewBulletSystem(w).(*BulletSystem)
	spirits := NewSpiritSystem(w).(*SpiritSystem)

	bullets.HandleEvent(event.GameEvent{Type: event.EventBulletSpawnRequest, Payload: &event.BulletSpawnRequestPayload{
		OriginX: 2.5, OriginY: 5.5, VelX: 1, MaxLifetime: parameter.TurretBulletLifetime,
	}})
	spirits.HandleEvent(event.GameEvent{Type: event.EventSpiritSpawnRequest, Domain: core.DomainPlayer,
		Payload: &event.SpiritSpawnRequestPayload{StartX: 2, StartY: 2, TargetX: 30, TargetY: 10, Char: 'x'}})
	bullet, spirit := w.Components.Bullet.Entities()[0], w.Components.Spirit.Entities()[0]

	alive := func(e core.Entity) bool { _, ok := w.GetComponentMask(e); return ok }
	moves, frames, lastProgress := 0, 0, 0.0
	for range 100 {
		tick.open()
		if alive(bullet) {
			bullets.Update()
			moves++
		}
		// Drawn as the spirit renderer draws it
		if timer, ok := w.Components.Timer.GetPtr(spirit); ok && !timer.Expired(component.TimerLifetime) {
			frames++
			lastProgress = min(float64(timer.Elapsed(component.TimerLifetime))/float64(parameter.SpiritAnimationDuration), 1)
		}
	}
	if want := int(parameter.TurretBulletLifetime / parameter.GameUpdateInterval); moves != want {
		t.Fatalf("bullet moved on %d ticks, want %d", moves, want)
	}
	if want := int(parameter.SpiritAnimationDuration / parameter.GameUpdateInterval); frames != want || lastProgress != 1 {
		t.Fatalf("spirit drew %d frames ending at %v, want %d ending at 1", frames, lastProgress, want)
	}
}

// TestDeadlinesEndOnTheirLastTick pins each deadline to the tick its owner acts:
// a drain beam completes on its 20th tick and a nugget beacon on its 12th, as
// their summed progress did; a missile detonates on its 60th tick, where its 60th
// move left it, and a gold sequence times out on the tick its countdown reaches
// zero.
func TestDeadlinesEndOnTheirLastTick(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	tick := newTimerTick(w)
	ticks := func(d time.Duration) int { return int(d / parameter.GameUpdateInterval) }

	// actsOn runs ticks until done reports the owner acted, returning that tick
	actsOn := func(update func(), done func([]event.GameEvent) bool) int {
		for n := 1; n <= 1000; n++ {
			tick.open()
			update()
			if done(w.Resources.Event.Queue.Consume()) {
				return n
			}
		}
		return 0
	}
	has := func(typ event.EventType) func([]event.GameEvent) bool {
		return func(events []event.GameEvent) bool {
			for _, ev := range events {
				if ev.Type == typ {
					return true
				}
			}
			return false
		}
	}

	materialize := NewMaterializeSystem(w).(*MaterializeSystem)
	for _, spawn := range []component.SpawnType{component.SpawnTypeDrain, component.SpawnTypeNugget} {
		materialize.HandleEvent(event.GameEvent{Type: event.EventMaterializeRequest, Domain: core.DomainPlayer,
			Payload: &event.MaterializeRequestPayload{X: 10, Y: 10, Type: spawn}})
		if got, want := actsOn(materialize.Update, has(event.EventMaterializeComplete)), ticks(spawn.Style().Duration); got != want {
			t.Fatalf("spawn type %d beam completed on tick %d, want %d", spawn, got, want)
		}
		if n := w.Components.Materialize.CountEntities(); n != 0 {
			t.Fatalf("completed beams left = %d, want none", n)
		}
	}

	missiles := NewMissileSystem(w).(*MissileSystem)
	missiles.HandleEvent(event.GameEvent{Type: event.EventMissileSpawnRequest,
		Payload: &event.MissileSpawnRequestPayload{OriginX: 20, OriginY: 12, Count: 1}})
	missile := w.Components.Missile.Entities()[0]
	kinetic, _ := w.Components.Kinetic.GetPtr(missile)
	kinetic.VelX, kinetic.VelY = 0, 0 // an orphan holding still, so only its deadline ends it
	if got, want := actsOn(missiles.Update, has(event.EventExplosionVisualRequest)), ticks(parameter.MissileMaxLifetime); got != want {
		t.Fatalf("orphaned missile detonated on tick %d, want %d", got, want)
	}

	gold := NewGoldSystem(w).(*GoldSystem)
	gold.HandleEvent(event.GameEvent{Type: event.EventGoldSpawnRequest})
	if !gold.active {
		t.Fatal("gold sequence did not spawn")
	}
	w.Resources.Event.Queue.Consume()
	if got, want := actsOn(gold.Update, has(event.EventGoldTimeout)), ticks(parameter.GoldDuration); got != want {
		t.Fatalf("gold sequence timed out on tick %d, want %d", got, want)
	}
}

// TestTimerCollectsAnExpiredLifetimeByState: a capture taken between the pass that
// expired a lifetime and the settle of its batch installs the expired slot alone,
// and the next pass still ends the entity; a death-protected one keeps living.
func TestTimerCollectsAnExpiredLifetimeByState(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	tick := newTimerTick(w)

	installed := w.CreateEntity(core.DomainShared)
	protected := w.CreateEntity(core.DomainShared)
	for _, e := range []core.Entity{installed, protected} {
		w.Components.Timer.SetComponent(e, component.TimerComponent{
			Slots: [component.TimerSlotCount]component.Countdown{component.TimerLifetime: {Duration: time.Second}},
		})
	}
	w.Components.Protection.SetComponent(protected, component.ProtectionComponent{Mask: component.ProtectFromDeath})

	tick.open()
	tick.open()
	if _, alive := w.GetComponentMask(installed); alive {
		t.Fatal("an installed expired lifetime was never collected")
	}
	timer, ok := w.Components.Timer.GetPtr(protected)
	if _, alive := w.GetComponentMask(protected); !alive || !ok || timer.Expired(component.TimerLifetime) {
		t.Fatal("a death-protected entity died, or kept an expired lifetime to collect every pass")
	}
}
