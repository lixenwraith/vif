package manifest

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestNoRegisteredRendererDrawsOutsideTheMap runs every renderer the manifest
// registers, at its real priority, on the geometry the out-of-bounds defect
// appears in: a terminal larger than the map, so the map is centred and leaves a
// margin nothing may draw into.
//
// The compositor clip is deliberately NOT applied here — that backstop has its
// own tests in internal/render. What this asserts is the layer under it: that a
// simulation renderer bounds itself, including when its own geometry overhangs
// the map, as the marker and ping fixtures below do. Renderers with nothing to
// draw in this world are covered only for surviving the frame.
//
// It is written over the registry rather than per renderer so a renderer added
// later is covered without anyone remembering to extend a list.
func TestNoRegisteredRendererDrawsOutsideTheMap(t *testing.T) {
	t.Parallel()

	const screenW, screenH = 80, 24
	world := engine.NewWorld()
	gameCtx := engine.NewGameContextWithClock(world, screenW, screenH, engine.NewManualClock())
	world.SetupLevel(20, 8, false, false)

	cfg := world.Resources.Config
	cursor := world.CreateEntity(core.DomainShared)
	world.Components.Cursor.SetComponent(cursor, component.CursorComponent{})
	world.Positions.SetPosition(cursor, component.PositionComponent{X: cfg.MapWidth / 2, Y: cfg.MapHeight / 2})
	world.Components.Ping.SetComponent(cursor, component.PingComponent{ShowCrosshair: true, GridActive: true})
	world.Resources.Player.Bind(0, cursor)
	world.Resources.Player.SetLocal(0)

	// Content whose drawn extent overhangs the map: a glyph and a sigil on the
	// last cell of each axis, and a marker rectangle deliberately hanging off the
	// bottom-right corner. Before the fix the marker's overhang landed in the
	// centring margin, because a projection into the margin reported itself
	// visible.
	glyph := world.CreateEntity(core.DomainShared)
	world.Components.Glyph.SetComponent(glyph, component.GlyphComponent{Rune: 'a'})
	world.Positions.SetPosition(glyph, component.PositionComponent{X: cfg.MapWidth - 1, Y: 0})

	sigil := world.CreateEntity(core.DomainShared)
	world.Components.Sigil.SetComponent(sigil, component.SigilComponent{Rune: '*', Color: color.RGB{R: 200}})
	world.Positions.SetPosition(sigil, component.PositionComponent{X: 0, Y: cfg.MapHeight - 1})

	marker := world.CreateEntity(core.DomainShared)
	world.Components.Marker.SetComponent(marker, component.MarkerComponent{
		X: cfg.MapWidth - 2, Y: cfg.MapHeight - 2, Width: 6, Height: 6,
		Shape: component.MarkerShapeRectangle, Color: color.RGB{G: 200}, Intensity: 1.0,
	})

	ctx := render.RenderContext{
		GameXOffset: 3, GameYOffset: 1,
		ViewportWidth: cfg.ViewportWidth, ViewportHeight: cfg.ViewportHeight,
		MapOffsetX: (cfg.ViewportWidth - cfg.MapWidth) / 2,
		MapOffsetY: (cfg.ViewportHeight - cfg.MapHeight) / 2,
		MapWidth:   cfg.MapWidth, MapHeight: cfg.MapHeight,
		CursorX: cfg.MapWidth / 2, CursorY: cfg.MapHeight / 2, CursorValid: true,
		ScreenWidth: screenW, ScreenHeight: screenH,
	}
	if ctx.MapOffsetX <= 0 || ctx.MapOffsetY <= 0 {
		t.Fatalf("this test needs a map smaller than the viewport; got %dx%d in %dx%d",
			cfg.MapWidth, cfg.MapHeight, cfg.ViewportWidth, cfg.ViewportHeight)
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, screenW, screenH)
	pf, area := ctx.PlayfieldRect(), ctx.GameAreaRect()

	for _, reg := range BuildRenderers(gameCtx) {
		if vt, ok := reg.Renderer.(render.VisibilityToggle); ok && !vt.IsVisible() {
			continue
		}
		buf.Clear()
		reg.Renderer.Render(ctx, buf)

		if !reg.Priority.ClipsToPlayfield() {
			continue
		}
		var zero terminal.Cell
		for y := range screenH {
			for x := range screenW {
				if buf.CellAt(x, y) != zero && !pf.Contains(x, y) {
					t.Fatalf("priority %d drew at screen (%d,%d), outside the map rect %+v",
						reg.Priority, x, y, pf)
				}
			}
		}
	}

	// And the margin the map is centred in is presented as out of play.
	buf.Clear()
	buf.SetVoidRegion(area, pf, visual.RgbVoid)
	buf.FlushToTerminal(nullTerminal{})
	for y := area.Y0; y < area.Y1; y++ {
		for x := area.X0; x < area.X1; x++ {
			want := visual.RgbBackground
			if !pf.Contains(x, y) {
				want = visual.RgbVoid
			}
			if got := buf.CellAt(x, y).Bg; got != want {
				t.Fatalf("game-area cell (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

// nullTerminal is a host that accepts a flush and does nothing with it
type nullTerminal struct{}

func (nullTerminal) Init() error                             { return nil }
func (nullTerminal) Fini()                                   {}
func (nullTerminal) Size() (int, int)                        { return 0, 0 }
func (nullTerminal) ResizeChan() <-chan terminal.ResizeEvent { return nil }
func (nullTerminal) ColorMode() terminal.ColorMode           { return terminal.ColorModeTrueColor }
func (nullTerminal) Flush([]terminal.Cell, int, int)         {}
func (nullTerminal) Clear(color.RGB)                         {}
func (nullTerminal) SetCursorVisible(bool)                   {}
func (nullTerminal) MoveCursor(int, int)                     {}
func (nullTerminal) Sync()                                   {}
func (nullTerminal) PollEvent() terminal.Event               { return terminal.Event{} }
func (nullTerminal) PostEvent(terminal.Event)                {}
func (nullTerminal) SetMouseMode(terminal.MouseMode) error   { return nil }
