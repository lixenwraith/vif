package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// IndicatorRenderer draws relative row and column indicators around the viewport.
type IndicatorRenderer struct {
	gameCtx *engine.GameContext
}

// NewIndicatorRenderer creates an indicator renderer for both axes.
func NewIndicatorRenderer(gameCtx *engine.GameContext) *IndicatorRenderer {
	return &IndicatorRenderer{
		gameCtx: gameCtx,
	}
}

// Render implements SystemRenderer.
func (r *IndicatorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	cursorVX, cursorVY := ctx.CursorViewportPos()
	inputMode := r.gameCtx.IsSearchMode() || r.gameCtx.IsCommandMode()

	// Rows and columns outside the map are not addressable by any motion, so the
	// gutters mark them as out of play rather than numbering them. Without this a
	// centred map would sit inside a black margin with numbered rows beside it.
	pf := ctx.PlayfieldViewportRect()

	// --- Row indicators (left gutter: padding, digit, padding) ---
	for y := range ctx.ViewportHeight {
		screenY := ctx.GameYOffset + y
		pad := visual.RgbBackground
		if y < pf.Y0 || y >= pf.Y1 {
			pad = visual.RgbVoid
		}
		for x := range ctx.GameXOffset {
			buf.SetWithBg(x, screenY, ' ', pad, pad)
		}
		if pad == visual.RgbVoid {
			continue
		}

		relativeNum := y - cursorVY
		absRelative := relativeNum
		if absRelative < 0 {
			absRelative = -absRelative
		}

		var ch rune
		var fg, bg color.RGB

		if relativeNum == 0 {
			ch = '0'
			if inputMode {
				fg = visual.RgbCursorNormal
				bg = visual.RgbBackground
			} else {
				fg = visual.RgbBlack
				bg = visual.RgbCursorNormal
			}
		} else {
			fg = visual.RgbIndicator
			bg = visual.RgbBackground

			if absRelative%10 == 0 {
				ch = rune('0' + (absRelative/10)%10)
			} else if absRelative%2 == 0 {
				ch = '─'
			} else {
				ch = ' '
			}
		}

		buf.SetWithBg(1, screenY, ch, fg, bg)
	}

	// --- Column indicators (bottom row) ---
	indicatorY := ctx.GameYOffset + ctx.ViewportHeight

	for x := range ctx.ViewportWidth {
		screenX := ctx.GameXOffset + x
		if x < pf.X0 || x >= pf.X1 {
			buf.SetWithBg(screenX, indicatorY, ' ', visual.RgbVoid, visual.RgbVoid)
			continue
		}

		relativeCol := x - cursorVX

		var ch rune
		var fg, bg color.RGB

		if relativeCol == 0 {
			ch = '0'
			if inputMode {
				fg = visual.RgbCursorNormal
				bg = visual.RgbBackground
			} else {
				fg = visual.RgbBlack
				bg = visual.RgbCursorNormal
			}
		} else {
			absRelative := relativeCol
			if absRelative < 0 {
				absRelative = -absRelative
			}
			if absRelative%10 == 0 {
				ch = rune('0' + (absRelative / 10 % 10))
			} else if absRelative%5 == 0 {
				ch = '|'
			} else {
				ch = ' '
			}
			fg = visual.RgbIndicator
			bg = visual.RgbBackground
		}
		buf.SetWithBg(screenX, indicatorY, ch, fg, bg)
	}

	// The corner under the row gutter is outside the map on both axes.
	for x := range ctx.GameXOffset {
		buf.SetWithBg(x, indicatorY, ' ', visual.RgbVoid, visual.RgbVoid)
	}
}
