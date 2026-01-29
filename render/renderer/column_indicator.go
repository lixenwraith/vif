package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
)

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
	indicatorY := ctx.GameYOffset + ctx.GameHeight

	for x := 0; x < ctx.GameWidth; x++ {
		screenX := ctx.GameXOffset + x
		relativeCol := x - ctx.CursorX

		var ch rune
		var fg, bg render.RGB

		if relativeCol == 0 {
			ch = '0'
			if r.gameCtx.IsSearchMode() || r.gameCtx.IsCommandMode() {
				fg = render.RgbCursorNormal
				bg = render.RgbBackground
			} else {
				fg = render.RgbBlack
				bg = render.RgbCursorNormal
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
			fg = render.RgbColumnIndicator
			bg = render.RgbBackground
		}
		buf.SetWithBg(screenX, indicatorY, ch, fg, bg)
	}

	// Clear line number area for indicator row
	for i := 0; i < ctx.GameXOffset; i++ {
		buf.SetWithBg(i, indicatorY, ' ', render.RgbBackground, render.RgbBackground)
	}
}