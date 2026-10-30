package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
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
