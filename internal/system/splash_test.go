package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TestTimerContentIsBoundedByItsComponentBuffer covers the crash surface even if
// an upstream deadline is malformed. time.Duration can represent ten decimal
// digits of seconds while SplashComponent reserves eight runes; the timer must
// never let that representation become an out-of-range component write.
func TestTimerContentIsBoundedByItsComponentBuffer(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())
	splash := NewSplashSystem(w).(*SplashSystem)

	splash.handleTimerSpawn(&event.SplashTimerRequestPayload{
		Duration: time.Duration(1<<63 - 1),
	})
	splash.Update()

	entities := w.Components.Splash.Entities()
	if len(entities) != 1 {
		t.Fatalf("timer splashes = %d, want 1", len(entities))
	}
	c, ok := w.Components.Splash.GetComponent(entities[0])
	if !ok {
		t.Fatal("timer splash has no component")
	}
	if c.Slot != component.SlotTimer {
		t.Fatalf("splash slot = %d, want timer", c.Slot)
	}
	if c.Length != parameter.SplashMaxLength {
		t.Fatalf("timer length = %d, want bounded length %d", c.Length, parameter.SplashMaxLength)
	}
}
