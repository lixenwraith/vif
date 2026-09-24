package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// TestGuttersNumberOnlyReachableRowsAndColumns keeps the chrome consistent with
// the margin beside it: a numbered row next to a black out-of-play band reads as
// a row the cursor can reach, and it cannot. The corner between the gutters is
// void only where one of them stops short of it.
func TestGuttersNumberOnlyReachableRowsAndColumns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		mapW, mapH int
		cornerBg   color.RGB
	}{
		{"centred", 10, 6, visual.RgbVoid},
		{"filling", 60, 30, visual.RgbBackground},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := engine.NewWorld()
			gameCtx := engine.NewGameContextWithClock(world, 80, 24, engine.NewManualClock())
			world.SetupLevel(tc.mapW, tc.mapH, false, false)

			cfg := world.Resources.Config
			ctx := render.RenderContext{
				GameXOffset: parameter.LeftMargin, GameYOffset: 1,
				ViewportWidth: 40, ViewportHeight: 20,
				MapOffsetX: max(40-cfg.MapWidth, 0) / 2, MapOffsetY: max(20-cfg.MapHeight, 0) / 2,
				MapWidth: cfg.MapWidth, MapHeight: cfg.MapHeight,
				CursorX: 4, CursorY: 2,
			}

			buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 60, 30)
			NewIndicatorRenderer(gameCtx).Render(ctx, buf)

			pf := ctx.PlayfieldViewportRect()

			// Row gutter: every column of it.
			for y := range ctx.ViewportHeight {
				inPlay := y >= pf.Y0 && y < pf.Y1
				for x := range ctx.GameXOffset {
					got := buf.CellAt(x, ctx.GameYOffset+y).Bg
					if !inPlay && got != visual.RgbVoid {
						t.Fatalf("out-of-play row %d gutter col %d = %v, want void %v", y, x, got, visual.RgbVoid)
					}
					if inPlay && got == visual.RgbVoid {
						t.Fatalf("reachable row %d gutter col %d was marked out of play", y, x)
					}
				}
			}

			// Column gutter: the row under the game area, and the corner under the row gutter.
			indicatorY := ctx.GameYOffset + ctx.ViewportHeight
			for x := range ctx.GameXOffset {
				if got := buf.CellAt(x, indicatorY).Bg; got != tc.cornerBg {
					t.Fatalf("corner col %d = %v, want %v", x, got, tc.cornerBg)
				}
			}
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
		})
	}
}
