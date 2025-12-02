package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// ColumnIndicatorsRenderer draws column position indicators
type ColumnIndicatorsRenderer struct {
	gameCtx *engine.GameContext
}

// NewColumnIndicatorsRenderer creates a column indicators renderer
func NewColumnIndicatorsRenderer(gameCtx *engine.GameContext) *ColumnIndicatorsRenderer {
	return &ColumnIndicatorsRenderer{
		gameCtx: gameCtx,
	}
}

// Render implements SystemRenderer
func (c *ColumnIndicatorsRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	indicatorY := ctx.GameY + ctx.GameHeight

	for x := 0; x < ctx.GameWidth; x++ {
		screenX := ctx.GameX + x
		relativeCol := x - ctx.CursorX

		var ch rune
		var fg, bg render.RGB

		if relativeCol == 0 {
			ch = '0'
			if c.gameCtx.IsSearchMode() || c.gameCtx.IsCommandMode() {
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
	for i := 0; i < ctx.GameX; i++ {
		buf.SetWithBg(i, indicatorY, ' ', render.RgbBackground, render.RgbBackground)
	}
}