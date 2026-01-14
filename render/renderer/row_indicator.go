package renderer

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// RowIndicatorRenderer draws relative row numbers
type RowIndicatorRenderer struct {
	gameCtx *engine.GameContext
}

// NewRowIndicatorRenderer creates a row indicator renderer
func NewRowIndicatorRenderer(gameCtx *engine.GameContext) *RowIndicatorRenderer {
	return &RowIndicatorRenderer{
		gameCtx: gameCtx,
	}
}

// Render implements SystemRenderer
func (r *RowIndicatorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskUI)

	for y := 0; y < ctx.GameHeight; y++ {
		relativeNum := y - ctx.CursorY
		absRelative := relativeNum
		if absRelative < 0 {
			absRelative = -absRelative
		}

		screenY := ctx.GameYOffset + y

		// Column 0: left padding (always empty, never highlighted)
		buf.SetWithBg(0, screenY, ' ', render.RgbBackground, render.RgbBackground)

		// Column 1: line indicator
		var ch rune
		var fg, bg render.RGB

		if relativeNum == 0 {
			ch = '0'
			if r.gameCtx.IsSearchMode() || r.gameCtx.IsCommandMode() {
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
			} else if absRelative%2 == 0 {
				ch = '─'
			} else {
				ch = ' '
			}
		}

		buf.SetWithBg(1, screenY, ch, fg, bg)
	}
}