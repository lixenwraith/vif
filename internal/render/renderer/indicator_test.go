package renderer

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestGuttersNumberOnlyReachableRowsAndColumns keeps the chrome consistent with
// the margin beside it: a numbered row next to a black out-of-play band reads as
// a row the cursor can reach, and it cannot.
func TestGuttersNumberOnlyReachableRowsAndColumns(t *testing.T) {
	t.Parallel()

	world := engine.NewWorld()
	gameCtx := engine.NewGameContextWithClock(world, 80, 24, engine.NewManualClock())
	world.SetupLevel(10, 6, false, false)

	cfg := world.Resources.Config
	ctx := render.RenderContext{
		GameXOffset: 3, GameYOffset: 1,
		ViewportWidth: 40, ViewportHeight: 20,
		MapOffsetX: (40 - cfg.MapWidth) / 2, MapOffsetY: (20 - cfg.MapHeight) / 2,
		MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
		CursorX: 4, CursorY: 2,
	}

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 60, 30)
	NewIndicatorRenderer(gameCtx).Render(ctx, buf)

	pf := ctx.PlayfieldViewportRect()

	// Row gutter: the two indicator columns.
	for y := range ctx.ViewportHeight {
		inPlay := y >= pf.Y0 && y < pf.Y1
		for _, x := range []int{0, 1} {
			got := buf.CellAt(x, ctx.GameYOffset+y).Bg
			if !inPlay && got != visual.RgbVoid {
				t.Fatalf("out-of-play row %d gutter col %d = %v, want void %v", y, x, got, visual.RgbVoid)
			}
			if inPlay && got == visual.RgbVoid {
				t.Fatalf("reachable row %d gutter col %d was marked out of play", y, x)
			}
		}
	}

	// Column gutter: the row under the game area.
	indicatorY := ctx.GameYOffset + ctx.ViewportHeight
	for x := range ctx.ViewportWidth {
		inPlay := x >= pf.X0 && x < pf.X1
		got := buf.CellAt(ctx.GameXOffset+x, indicatorY).Bg
		if !inPlay && got != visual.RgbVoid {
			t.Fatalf("out-of-play column %d = %v, want void %v", x, got, visual.RgbVoid)
		}
		if inPlay && got == visual.RgbVoid {
			t.Fatalf("reachable column %d was marked out of play", x)
		}
	}
}
