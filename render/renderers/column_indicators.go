package renderers

import (
	"github.com/gdamore/tcell/v2"
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
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)
	indicatorStyle := defaultStyle.Foreground(render.RgbColumnIndicator)

	indicatorY := ctx.GameY + ctx.GameHeight

	for x := 0; x < ctx.GameWidth; x++ {
		screenX := ctx.GameX + x
		relativeCol := x - ctx.CursorX
		var ch rune
		var colStyle tcell.Style

		if relativeCol == 0 {
			ch = '0'
			if c.gameCtx.IsSearchMode() || c.gameCtx.IsCommandMode() {
				colStyle = defaultStyle.Foreground(render.RgbCursorNormal)
			} else {
				colStyle = defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbCursorNormal)
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
			colStyle = indicatorStyle
		}
		buf.Set(screenX, indicatorY, ch, colStyle)
	}

	// Clear line number area for indicator row
	for i := 0; i < ctx.GameX; i++ {
		buf.Set(i, indicatorY, ' ', defaultStyle)
	}
}