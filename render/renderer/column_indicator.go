package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TODO: merge row and column indicator renderers
// ColumnIndicatorRenderer draws relative column numbers
type ColumnIndicatorRenderer struct {
	gameCtx *engine.GameContext
}

// NewColumnIndicatorRenderer creates a column indicator renderer
func NewColumnIndicatorRenderer(gameCtx *engine.GameContext) *ColumnIndicatorRenderer {
	return &ColumnIndicatorRenderer{
		gameCtx: gameCtx,
	}
}

// Render implements SystemRenderer
func (r *ColumnIndicatorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)
	indicatorY := ctx.GameYOffset + ctx.ViewportHeight

	// Get cursor position in viewport coordinates
	cursorVX, _ := ctx.CursorViewportPos()

	for x := 0; x < ctx.ViewportWidth; x++ {
		screenX := ctx.GameXOffset + x
		relativeCol := x - cursorVX

		var ch rune
		var fg, bg terminal.RGB

		if relativeCol == 0 {
			ch = '0'
			if r.gameCtx.IsSearchMode() || r.gameCtx.IsCommandMode() {
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
			fg = visual.RgbColumnIndicator
			bg = visual.RgbBackground
		}
		buf.SetWithBg(screenX, indicatorY, ch, fg, bg)
	}

	// Clear line number area for indicator row
	for i := 0; i < ctx.GameXOffset; i++ {
		buf.SetWithBg(i, indicatorY, ' ', visual.RgbBackground, visual.RgbBackground)
	}
}