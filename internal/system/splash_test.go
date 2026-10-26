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

// TestTimerContentIsBoundedByItsComponentBuffer covers the crash surface even if
// an upstream deadline is malformed. time.Duration can represent ten decimal
// digits of seconds while SplashComponent reserves eight runes; the timer must
// never let that representation become an out-of-range component write.
func TestTimerContentIsBoundedByItsComponentBuffer(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())
	splash := ready(NewSplashSystem(w)).(*SplashSystem)

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

// A countdown whose anchor has left the map is leaving with it: until the next
// update retires it, it holds no space against magnifiers or other countdowns.
func TestTimerWithoutItsAnchorHoldsNoSpace(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())
	splash := ready(NewSplashSystem(w)).(*SplashSystem)

	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 60, Y: 20})
	splash.handleTimerSpawn(&event.SplashTimerRequestPayload{AnchorEntity: header, Duration: 10 * time.Second})
	if boxes := splash.getTimerBBoxes(0); len(boxes) != 1 {
		t.Fatalf("anchored countdown boxes = %d, want 1", len(boxes))
	}

	w.DestroyEntity(header)
	if boxes := splash.getTimerBBoxes(0); len(boxes) != 0 {
		t.Fatalf("countdown without its anchor holds %v", boxes)
	}
}

// The magnifier shows the glyph under the cursor whichever domain holds it: a gold
// member is a shared glyph, and it is the one the typist is about to type.
func TestMagnifierShowsAGoldMember(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	splash := ready(NewSplashSystem(w)).(*SplashSystem)
	member := w.CreateEntity(core.DomainShared)
	w.Components.Glyph.SetComponent(member, component.GlyphComponent{Rune: 'G', Type: component.GlyphGold})
	w.Positions.SetPosition(member, component.PositionComponent{X: 5, Y: 5})

	splash.handleCursorMoved(&event.CursorMovedPayload{X: 5, Y: 5})
	entities := w.Components.Splash.Entities()
	if len(entities) != 1 {
		t.Fatalf("splashes = %d, want the magnifier", len(entities))
	}
	c, _ := w.Components.Splash.GetComponent(entities[0])
	if c.Slot != component.SlotMagnifier || c.Content[0] != 'G' {
		t.Fatalf("splash = slot %d %q, want the magnifier showing G", c.Slot, c.Content[0])
	}
	if !splash.validateMagnifier(entities[0], &c) {
		t.Fatal("the magnifier over a gold member was retired")
	}
}
