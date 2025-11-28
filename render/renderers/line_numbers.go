package renderers

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// LineNumbersRenderer draws relative line numbers.
type LineNumbersRenderer struct {
	lineNumWidth int
	gameCtx      *engine.GameContext
}

// NewLineNumbersRenderer creates a line numbers renderer.
func NewLineNumbersRenderer(lineNumWidth int, gameCtx *engine.GameContext) *LineNumbersRenderer {
	return &LineNumbersRenderer{
		lineNumWidth: lineNumWidth,
		gameCtx:      gameCtx,
	}
}

// Render implements SystemRenderer.
func (l *LineNumbersRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)
	lineNumStyle := defaultStyle.Foreground(render.RgbLineNumbers)

	for y := 0; y < ctx.GameHeight; y++ {
		relativeNum := y - ctx.CursorY
		if relativeNum < 0 {
			relativeNum = -relativeNum
		}
		lineNum := fmt.Sprintf("%*d", l.lineNumWidth, relativeNum)

		var numStyle tcell.Style
		if relativeNum == 0 {
			if l.gameCtx.IsSearchMode() || l.gameCtx.IsCommandMode() {
				numStyle = defaultStyle.Foreground(render.RgbCursorNormal)
			} else {
				numStyle = defaultStyle.Foreground(tcell.ColorBlack).Background(render.RgbCursorNormal)
			}
		} else {
			numStyle = lineNumStyle
		}

		screenY := ctx.GameY + y
		for i, ch := range lineNum {
			buf.Set(i, screenY, ch, numStyle)
		}
	}
}

// UpdateLineNumWidth updates the line number column width.
func (l *LineNumbersRenderer) UpdateLineNumWidth(width int) {
	l.lineNumWidth = width
}
