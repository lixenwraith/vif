package system

import (
	"math"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
)

// TestAGrowingPulseStrikesOnlyItsDrawnSize: a pulse strikes inside the size it has
// swollen to, the size its glow draws, so a cursor three quarters of its radius out
// is struck only once it has grown that far
func TestAGrowingPulseStrikesOnlyItsDrawnSize(t *testing.T) {
	w, near, far := testCursorWorld(t)
	w.Resources.Time.DeltaTime = 50 * time.Millisecond
	pulses := ready(NewPulseSystem(w)).(*PulseSystem)
	pulse := w.CreateEntity(core.DomainShared)
	w.Components.Pulse.SetComponent(pulse, component.PulseComponent{
		PulseSpec: component.PulseSpec{Radius: 21, Duration: time.Second, Grow: 500 * time.Millisecond,
			HitInterval: 50 * time.Millisecond, Style: component.PulseGlow},
		X: 0, Y: 5, Hostile: true, Damage: component.CursorDamage{HeatDelta: -1},
	})

	// Cursors stand 5 and 15 cells out; the pulse reaches 10.5 + 1.05 cells a tick
	for tick := 1; tick <= 6; tick++ {
		pulses.Update()
		struck := make(map[core.Entity]bool)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.HeatAddRequestPayload); ok {
				struck[p.Entity] = true
			}
		}
		if !struck[near] || struck[far] != (tick >= 5) {
			t.Fatalf("tick %d: struck = %v, want %d always and %d from tick 5", tick, struck, near, far)
		}
	}
}

// TestAMountRequestShapesItsPulse: a scenario's mount overrides its kind's pulse and
// cursor damage field by field, by the names it writes, and each field it leaves
// zero is the kind's
func TestAMountRequestShapesItsPulse(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	mounts := ready(NewMountSystem(w)).(*MountSystem)
	payload, err := event.AuthoredPayload(event.PayloadNames(component.Declared{}), event.EventMountRequest, map[string]any{
		"weapon": "disruptor", "energy": int64(900),
		"pulse": map[string]any{"radius": 12.0, "sustained": true, "hit_interval_ms": int64(100), "style": "hidden", "attack": "explosion"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := payload.(*event.MountRequestPayload)
	request.Host = w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(request.Host, component.PositionComponent{X: 10, Y: 5})
	mounts.HandleEvent(event.GameEvent{Type: event.EventMountRequest, Payload: request})
	mounts.Update()

	live := w.Components.Pulse.Entities()
	if len(live) != 1 {
		t.Fatalf("pulses = %v, want one", live)
	}
	got, _ := w.Components.Pulse.GetComponent(live[0])
	want := component.PulseSpec{Radius: 12, Duration: parameter.PulseDuration, Sustained: true,
		HitInterval: 100 * time.Millisecond, Style: component.PulseHidden, Attack: component.CombatAttackExplosion}
	if got.PulseSpec != want || got.Damage != (component.CursorDamage{EnergyDrain: 900, HeatDelta: -parameter.HostedDisruptorHeat}) {
		t.Fatalf("pulse = %+v damage %+v, want %+v and 900 energy at the kind's heat", got.PulseSpec, got.Damage, want)
	}
}

// TestADeathWaveRunsFromItsCursorToTheMapsFarCorner: a request fires one slide pulse
// of the death wave's family on its cursor's cell, reaching the corner farthest from
// it; the family ends a swarm and has no row for a quasar
func TestADeathWaveRunsFromItsCursorToTheMapsFarCorner(t *testing.T) {
	w, cursor, _ := testCursorWorld(t)
	weapons := ready(NewWeaponSystem(w)).(*WeaponSystem)
	weapons.HandleEvent(event.GameEvent{Type: event.EventDeathWaveRequest, Payload: &event.DeathWaveRequestPayload{Entity: cursor}})
	waves := w.Components.Pulse.Entities()
	if len(waves) != 1 {
		t.Fatalf("the request fired %d pulses, want one", len(waves))
	}
	wave, _ := w.Components.Pulse.GetComponent(waves[0])
	at, _ := w.Positions.GetPosition(cursor)
	cfg := w.Resources.Config
	corner := math.Hypot(float64(cfg.MapWidth-1-at.X), cfg.CellAspect.Circular(float64(cfg.MapHeight-1-at.Y)))
	if wave.X != at.X || wave.Y != at.Y || wave.Owner != cursor || wave.Radius < corner ||
		wave.Style != component.PulseSlide || wave.Attack != component.CombatAttackDeathWave {
		t.Fatalf("the death wave is %+v, want a slide pulse of its family on (%d,%d) reaching %.1f", wave, at.X, at.Y, corner)
	}
	lethal := profile.Attack(component.CombatAttackDeathWave, component.CombatEntityCursor, component.CombatEntitySwarm)
	if lethal == nil || lethal.DamageValue < parameter.CombatInitialHPSwarm ||
		profile.Attack(component.CombatAttackDeathWave, component.CombatEntityCursor, component.CombatEntityQuasar) != nil {
		t.Fatal("the death wave's family does not end a swarm, or has a row for a quasar")
	}
}
