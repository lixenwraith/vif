package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// LineNumbersRenderer draws relative line numbers
type LineNumbersRenderer struct {
	gameCtx *engine.GameContext
}

// NewLineNumbersRenderer creates a line numbers renderer
func NewLineNumbersRenderer(gameCtx *engine.GameContext) *LineNumbersRenderer {
	return &LineNumbersRenderer{
		gameCtx: gameCtx,
	}
}

// Render implements SystemRenderer
func (l *LineNumbersRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)

	for y := 0; y < ctx.GameHeight; y++ {
		relativeNum := y - ctx.CursorY
		absRelative := relativeNum
		if absRelative < 0 {
			absRelative = -absRelative
		}

		screenY := ctx.GameY + y

		// Column 0: left padding (always empty, never highlighted)
		buf.SetWithBg(0, screenY, ' ', render.RgbBackground, render.RgbBackground)

		// Column 1: line indicator
		var ch rune
		var fg, bg render.RGB

		if relativeNum == 0 {
			ch = '0'
			if l.gameCtx.IsSearchMode() || l.gameCtx.IsCommandMode() {
				fg = render.RgbCursorNormal
				bg = render.RgbBackground
			} else {
				fg = render.RgbBlack
				bg = render.RgbCursorNormal
			}
		} else {
			fg = render.RgbLineNumbers
			bg = render.RgbBackground

			if absRelative%10 == 0 {
				ch = rune('0' + (absRelative/10)%10)
			} else if absRelative%5 == 0 {
				ch = '-'
			} else {
				ch = ' '
			}
		}

		buf.SetWithBg(1, screenY, ch, fg, bg)
	}
}