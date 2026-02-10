package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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

	// --- Row indicators (left gutter) ---
	for y := 0; y < ctx.ViewportHeight; y++ {
		relativeNum := y - cursorVY
		absRelative := relativeNum
		if absRelative < 0 {
			absRelative = -absRelative
		}

		screenY := ctx.GameYOffset + y

		// Column 0: left padding (always empty, never highlighted)
		buf.SetWithBg(0, screenY, ' ', visual.RgbBackground, visual.RgbBackground)

		// Column 1: line indicator
		var ch rune
		var fg, bg terminal.RGB

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

	for x := 0; x < ctx.ViewportWidth; x++ {
		screenX := ctx.GameXOffset + x
		relativeCol := x - cursorVX

		var ch rune
		var fg, bg terminal.RGB

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

	// Clear line number area for indicator row
	for i := 0; i < ctx.GameXOffset; i++ {
		buf.SetWithBg(i, indicatorY, ' ', visual.RgbBackground, visual.RgbBackground)
	}
}