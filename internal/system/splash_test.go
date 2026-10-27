package system

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/asset"
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

// countdownInk is the map cells a countdown's digits ink where they stand
func countdownInk(w *engine.World, c component.SplashComponent) []component.PositionComponent {
	pos, _ := w.Positions.GetPosition(c.AnchorEntity)
	var cells []component.PositionComponent
	for i := range c.Length {
		glyph := asset.SplashFont[c.Content[i]-' ']
		for row := range parameter.SplashCharHeight {
			for col := range parameter.SplashCharWidth {
				if asset.SplashInk(glyph, col, row) {
					cells = append(cells, component.PositionComponent{
						X: pos.X + c.OffsetX + i*parameter.SplashCharWidth + col, Y: pos.Y + c.OffsetY + row})
				}
			}
		}
	}
	return cells
}

// A countdown is placed by its ink, not by its cells: below an open body its digit
// stands the padding clear of the body's margin and no further, and in a corridor
// a row lower than the font's cells it still draws clear of the walls.
func TestACountdownIsPlacedByItsInk(t *testing.T) {
	newCountdown := func(t *testing.T, x, y int, walled func(x, y int) bool) (*engine.World, component.SplashComponent) {
		t.Helper()
		w := engine.NewWorld()
		engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())
		cfg := w.Resources.Config
		for wy := range cfg.MapHeight {
			for wx := range cfg.MapWidth {
				if walled(wx, wy) {
					e := w.CreateEntity(core.DomainShared)
					w.Positions.SetPosition(e, component.PositionComponent{X: wx, Y: wy})
					w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockAll})
				}
			}
		}
		splash := ready(NewSplashSystem(w)).(*SplashSystem)
		header := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(header, component.PositionComponent{X: x, Y: y})
		splash.handleTimerSpawn(&event.SplashTimerRequestPayload{AnchorEntity: header, MarginBottom: 1, Duration: 3 * time.Second})
		c, _ := w.Components.Splash.GetComponent(w.Components.Splash.Entities()[0])
		return w, c
	}

	t.Run("open", func(t *testing.T) {
		w, c := newCountdown(t, 60, 10, func(int, int) bool { return false })
		top := 40
		for _, cell := range countdownInk(w, c) {
			top = min(top, cell.Y)
		}
		if want := 10 + 1 + parameter.SplashTimerPadding; top != want {
			t.Fatalf("the digit's ink starts on row %d, want %d: the body's margin and the padding below it", top, want)
		}
	})

	t.Run("corridor", func(t *testing.T) {
		const ceiling, floor = 4, 4 + parameter.SplashCharHeight // rows 5 to 15 open
		w, c := newCountdown(t, 60, 5, func(_, y int) bool { return y <= ceiling || y >= floor })
		for _, cell := range countdownInk(w, c) {
			if w.Positions.HasBlockingWallAt(cell.X, cell.Y, component.WallBlockAll) {
				t.Fatalf("the digit inks (%d, %d), a wall, in a corridor of rows %d to %d", cell.X, cell.Y, ceiling+1, floor-1)
			}
		}
	})
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
	if splash.fillTimerBoxes(); len(splash.boxes) != 1 {
		t.Fatalf("anchored countdown boxes = %d, want 1", len(splash.boxes))
	}

	w.DestroyEntity(header)
	if splash.fillTimerBoxes(); len(splash.boxes) != 0 {
		t.Fatalf("countdown without its anchor holds %v", splash.boxes)
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
