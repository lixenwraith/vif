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
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

// countdownInk is the map cells a countdown's digits ink where they stand
func countdownInk(w *engine.World, c component.SplashComponent) []component.PositionComponent {
	pos, _ := w.Positions.GetPosition(c.AnchorEntity)
	face := &asset.SplashFaces[c.Font]
	pack := face.Packed(w.Resources.Config.TextConsole)
	var cells []component.PositionComponent
	for i := range c.Length {
		glyph, _ := face.Glyph(c.Content[i])
		for row := range face.Height {
			for col := range face.Width {
				if face.Ink(glyph, col, row) {
					cells = append(cells, component.PositionComponent{
						X: pos.X + c.OffsetX + i*face.Width + col, Y: pos.Y + c.OffsetY + row/pack})
				}
			}
		}
	}
	return cells
}

// newCountdown spawns a countdown anchored at x, y on a 120 by 40 map whose walls
// walled names, and returns its component
func newCountdown(t *testing.T, x, y int, font component.SplashFont, walled func(x, y int) bool) (*engine.World, component.SplashComponent) {
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
	splash.handleTimerSpawn(&event.SplashTimerRequestPayload{AnchorEntity: header, Font: font, MarginBottom: 1, Duration: 3 * time.Second})
	c, _ := w.Components.Splash.GetComponent(w.Components.Splash.Entities()[0])
	return w, c
}

// A countdown is placed by its ink, not by its cells: below an open body its digit
// stands the padding clear of the body's margin and no further, and in a corridor
// a row lower than the font's cells it still draws clear of the walls.
func TestACountdownIsPlacedByItsInk(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		w, c := newCountdown(t, 60, 10, component.SplashLarge, func(int, int) bool { return false })
		top := 40
		for _, cell := range countdownInk(w, c) {
			top = min(top, cell.Y)
		}
		if want := 10 + 1 + parameter.SplashTimerPadding; top != want {
			t.Fatalf("the digit's ink starts on row %d, want %d: the body's margin and the padding below it", top, want)
		}
	})

	t.Run("corridor", func(t *testing.T) {
		const ceiling, floor = 4, 4 + 12 // rows 5 to 15 open, the large face's cells
		w, c := newCountdown(t, 60, 5, component.SplashLarge, func(_, y int) bool { return y <= ceiling || y >= floor })
		if c.Font != component.SplashLarge {
			t.Fatalf("the countdown took face %d in a corridor its large digits fit", c.Font)
		}
		for _, cell := range countdownInk(w, c) {
			if w.Positions.Blocks(cell.X, cell.Y, component.WallBlockAll) {
				t.Fatalf("the digit inks (%d, %d), a wall, in a corridor of rows %d to %d", cell.X, cell.Y, ceiling+1, floor-1)
			}
		}
	})
}

// A large countdown with no clear slot takes the small face, which stands clear
// where the large digits cannot, and a countdown asked for small is small anywhere.
func TestACountdownWithNoRoomTakesTheSmallFace(t *testing.T) {
	const ceiling, floor = 4, 11 // rows 5 to 10 open: the small digits' four fit under the header, the large nine do not
	w, c := newCountdown(t, 60, 5, component.SplashLarge, func(_, y int) bool { return y <= ceiling || y >= floor })
	if c.Font != component.SplashSmall {
		t.Fatalf("the countdown kept face %d in a corridor of six rows", c.Font)
	}
	for _, cell := range countdownInk(w, c) {
		if w.Positions.Blocks(cell.X, cell.Y, component.WallBlockAll) {
			t.Fatalf("the small digit inks (%d, %d), a wall, in a corridor of rows %d to %d", cell.X, cell.Y, ceiling+1, floor-1)
		}
	}

	if _, c := newCountdown(t, 60, 10, component.SplashSmall, func(int, int) bool { return false }); c.Font != component.SplashSmall {
		t.Fatalf("a countdown asked for the small face took face %d in the open", c.Font)
	}
}

// A text stands at its origin cell: a second text written there replaces it, and an
// empty one clears it. A zero colour is the splash white.
func TestATextStandsAtItsOriginUntilReplacedOrCleared(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 120, 40, engine.NewManualClock())
	splash := ready(NewSplashSystem(w)).(*SplashSystem)
	write := func(text string) {
		splash.handleTextRequest(&event.SplashTextPayload{Text: text, Font: component.SplashSmall, OriginX: 10, OriginY: 20})
	}

	write("press a")
	write("press w")
	entities := w.Components.Splash.Entities()
	if len(entities) != 1 {
		t.Fatalf("texts at one origin = %d, want the second in place of the first", len(entities))
	}
	first := entities[0]
	c, _ := w.Components.Splash.GetComponent(first)
	if got := string(c.Content[:c.Length]); got != "press w" || c.Slot != component.SlotText || c.Font != component.SplashSmall {
		t.Fatalf("text = %q slot %d face %d, want \"press w\" as a small text", got, c.Slot, c.Font)
	}
	if c.Color != visual.RgbSplashWhite || c.AnchorX != 10 || c.AnchorY != 20 {
		t.Fatalf("text stands at (%d, %d) in %v, want its origin in the splash white", c.AnchorX, c.AnchorY, c.Color)
	}

	write("")
	write("press e")
	entities = w.Components.Splash.Entities()
	if len(entities) != 1 || entities[0] == first {
		t.Fatalf("texts after a clear and a write = %v, want one new entity, the cleared one gone at once", entities)
	}
	if n := countEvents(w.Resources.Event.Queue.Consume(), event.EventDeathBatch); n != 0 {
		t.Fatalf("clearing the text raised %d deaths, want it gone at once", n)
	}
}

// An unanchored countdown is drawn beside its origin cell, and each origin cell
// holds a countdown of its own.
func TestAnUnanchoredCountdownStandsBesideItsOrigin(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 160, 60, engine.NewManualClock())
	splash := ready(NewSplashSystem(w)).(*SplashSystem)
	for _, x := range []int{40, 120} {
		splash.handleTimerSpawn(&event.SplashTimerRequestPayload{OriginX: x, OriginY: 40, Duration: 3 * time.Second})
	}
	if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
		t.Fatalf("the second countdown raised %v, want the first left standing", events)
	}

	near := 2 * asset.SplashFaces[component.SplashLarge].Width
	for _, e := range w.Components.Splash.Entities() {
		c, _ := w.Components.Splash.GetComponent(e)
		if dx, dy := c.OffsetX, c.OffsetY; c.AnchorY != 40 || dx < -near || dx > near || dy < -near || dy > near {
			t.Fatalf("the countdown of (%d, %d) is drawn from (%d, %d), want within %d cells",
				c.AnchorX, c.AnchorY, c.AnchorX+dx, c.AnchorY+dy, near)
		}
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
	if splash.fillBoxes(); len(splash.boxes) != 1 {
		t.Fatalf("anchored countdown boxes = %d, want 1", len(splash.boxes))
	}

	w.DestroyEntity(header)
	if splash.fillBoxes(); len(splash.boxes) != 0 {
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
