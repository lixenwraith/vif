package renderers

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// LineNumbersRenderer draws relative line numbers
type LineNumbersRenderer struct {
	lineNumWidth int
	gameCtx      *engine.GameContext
}

// NewLineNumbersRenderer creates a line numbers renderer
func NewLineNumbersRenderer(lineNumWidth int, gameCtx *engine.GameContext) *LineNumbersRenderer {
	return &LineNumbersRenderer{
		lineNumWidth: lineNumWidth,
		gameCtx:      gameCtx,
	}
}

// Render implements SystemRenderer
func (l *LineNumbersRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	for y := 0; y < ctx.GameHeight; y++ {
		relativeNum := y - ctx.CursorY
		if relativeNum < 0 {
			relativeNum = -relativeNum
		}
		lineNum := fmt.Sprintf("%*d", l.lineNumWidth, relativeNum)

		var fg, bg core.RGB
		if relativeNum == 0 {
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
		}

		screenY := ctx.GameY + y
		for i, ch := range lineNum {
			buf.SetWithBg(i, screenY, ch, fg, bg)
		}
	}
}

// UpdateLineNumWidth updates the line number column width
func (l *LineNumbersRenderer) UpdateLineNumWidth(width int) {
	l.lineNumWidth = width
}