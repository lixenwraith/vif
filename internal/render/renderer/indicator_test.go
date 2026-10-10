package renderer

import (
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// TestGuttersNumberOnlyReachableRowsAndColumns keeps the chrome consistent with
// the margin beside it: a numbered row next to a black out-of-play band reads as
// a row the cursor can reach, and it cannot. Every other gutter cell, the corner
// included where one gutter stops short of it, is left for the frame's void.
func TestGuttersNumberOnlyReachableRowsAndColumns(t *testing.T) {
	t.Parallel()

	var undrawn terminal.Cell
	for _, tc := range []struct {
		name       string
		mapW, mapH int
		corner     terminal.Cell
	}{
		{"centred", 10, 6, undrawn},
		{"filling", 60, 30, terminal.Cell{Rune: ' ', Fg: visual.RgbBackground, Bg: visual.RgbBackground}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world := engine.NewWorld()
			gameCtx := engine.NewGameContextWithClock(world, 80, 24, engine.NewManualClock())
			world.SetupLevel(tc.mapW, tc.mapH, false, false, false)

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
					got := buf.CellAt(x, ctx.GameYOffset+y)
					if !inPlay && got != undrawn {
						t.Fatalf("out-of-play row %d gutter col %d drawn as %+v", y, x, got)
					}
					if inPlay && got.Bg != visual.RgbBackground && got.Bg != visual.RgbCursorNormal {
						t.Fatalf("reachable row %d gutter col %d = %+v, want a gutter cell", y, x, got)
					}
				}
			}

			// Column gutter: the row under the game area, and the corner under the row gutter.
			indicatorY := ctx.GameYOffset + ctx.ViewportHeight
			for x := range ctx.GameXOffset {
				if got := buf.CellAt(x, indicatorY); got != tc.corner {
					t.Fatalf("corner col %d = %+v, want %+v", x, got, tc.corner)
				}
			}
			for x := range ctx.ViewportWidth {
				inPlay := x >= pf.X0 && x < pf.X1
				got := buf.CellAt(ctx.GameXOffset+x, indicatorY)
				if !inPlay && got != undrawn {
					t.Fatalf("out-of-play column %d drawn as %+v", x, got)
				}
				if inPlay && got.Bg != visual.RgbBackground && got.Bg != visual.RgbCursorNormal {
					t.Fatalf("reachable column %d = %+v, want a gutter cell", x, got)
				}
			}
		})
	}
}
