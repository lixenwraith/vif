package renderer

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// A countdown follows its anchor header and is not drawn once the header has left
// the map. The splash system retires it on its next update; until then its unset
// AnchorX/Y put it in the playfield's top-left corner.
func TestAnchoredSplashIsNotDrawnWithoutItsAnchor(t *testing.T) {
	t.Parallel()
	w := engine.NewWorld()
	gameCtx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
	rc := peerContext(gameCtx)

	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: 30, Y: 5})
	timer := component.SplashComponent{
		Length:       1,
		Color:        visual.RgbSplashWhite,
		AnchorEntity: header,
		OffsetX:      4,
		OffsetY:      2,
		Slot:         component.SlotTimer,
	}
	timer.Content[0] = '8'
	w.Components.Splash.SetComponent(w.CreateEntity(core.DomainPlayer), timer)

	r := NewSplashRenderer(gameCtx)
	painted := func() (n, minX, minY int) {
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
		r.Render(rc, buf)
		minX, minY = 80, 24
		for y := range 24 {
			for x := range 80 {
				if buf.CellAt(x, y).Bg == visual.RgbSplashWhite {
					n++
					minX, minY = min(minX, x), min(minY, y)
				}
			}
		}
		return n, minX, minY
	}

	originX, originY, _ := rc.MapToScreen(34, 7)
	n, minX, minY := painted()
	if n == 0 {
		t.Fatal("countdown not drawn beside its anchor")
	}
	if minX < originX || minY < originY {
		t.Fatalf("countdown drawn from screen (%d,%d), want at or after its anchor's (%d,%d)", minX, minY, originX, originY)
	}

	w.DestroyEntity(header)
	if n, minX, minY := painted(); n != 0 {
		t.Fatalf("countdown without its anchor drew %d cells from screen (%d,%d)", n, minX, minY)
	}
}

// The small face packs two pixel rows a cell where the host draws half blocks, a
// cell inked in one row as a half block in the colour, and a text console, whose
// fonts lack them, draws it a cell a pixel.
func TestTheSmallFaceDrawsHalfBlocksOffAConsole(t *testing.T) {
	t.Parallel()
	for _, console := range []bool{false, true} {
		w := engine.NewWorld()
		gameCtx := engine.NewGameContextWithClock(w, 80, 24, engine.NewManualClock())
		gameCtx.World.Resources.Config.TextConsole = console
		text := component.SplashComponent{Length: 1, Color: visual.RgbSplashWhite, Font: component.SplashSmall, AnchorX: 10, AnchorY: 2, Slot: component.SlotText}
		text.Content[0] = '1'
		w.Components.Splash.SetComponent(w.CreateEntity(core.DomainPlayer), text)

		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 80, 24)
		NewSplashRenderer(gameCtx).Render(peerContext(gameCtx), buf)
		whole, half, minY, maxY := 0, 0, 24, -1
		for y := range 24 {
			for x := range 80 {
				cell := buf.CellAt(x, y)
				switch {
				case cell.Bg == visual.RgbSplashWhite:
					whole++
				case (cell.Rune == '▀' || cell.Rune == '▄') && cell.Fg == visual.RgbSplashWhite:
					half++
				default:
					continue
				}
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
		rows := maxY - minY + 1
		if console && (half != 0 || whole == 0 || rows != 8) {
			t.Fatalf("console: the digit drew %d whole and %d half cells over %d rows, want its eight pixel rows as whole cells", whole, half, rows)
		}
		if !console && (half == 0 || whole == 0 || rows != 4) {
			t.Fatalf("emulator: the digit drew %d whole and %d half cells over %d rows, want its eight pixel rows packed in four with half blocks", whole, half, rows)
		}
	}
}
