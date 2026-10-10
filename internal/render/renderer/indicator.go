package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
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

// Render implements SystemRenderer. The gutters number only the rows and columns
// the map covers, since no motion reaches the others; every gutter cell left
// undrawn is part of the frame the buffer fills as void.
func (r *IndicatorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	cursorVX, cursorVY := ctx.CursorViewportPos()
	inputMode := r.gameCtx.IsSearchMode() || r.gameCtx.IsCommandMode()
	pf := ctx.PlayfieldViewportRect()

	// Row indicators: the left gutter, its digit against the game area
	for y := pf.Y0; y < pf.Y1; y++ {
		screenY := ctx.GameYOffset + y
		for x := range ctx.GameXOffset - 1 {
			buf.SetWithBg(x, screenY, ' ', visual.RgbBackground, visual.RgbBackground)
		}
		ch, fg, bg := indicatorCell(y-cursorVY, inputMode, '─', 2)
		buf.SetWithBg(ctx.GameXOffset-1, screenY, ch, fg, bg)
	}

	// Column indicators: the row under the game area
	indicatorY := ctx.GameYOffset + ctx.ViewportHeight
	for x := pf.X0; x < pf.X1; x++ {
		ch, fg, bg := indicatorCell(x-cursorVX, inputMode, '|', 5)
		buf.SetWithBg(ctx.GameXOffset+x, indicatorY, ch, fg, bg)
	}

	// The corner joins the two gutters only where both reach it
	if pf.Y1 == ctx.ViewportHeight && pf.X0 == 0 {
		for x := range ctx.GameXOffset {
			buf.SetWithBg(x, indicatorY, ' ', visual.RgbBackground, visual.RgbBackground)
		}
	}
}

// indicatorCell returns the gutter cell rel rows or columns from the cursor: its
// own '0', the tens digit every ten, and minor every minorEvery between
func indicatorCell(rel int, inputMode bool, minor rune, minorEvery int) (rune, color.RGB, color.RGB) {
	if rel == 0 {
		if inputMode {
			return '0', visual.RgbCursorNormal, visual.RgbBackground
		}
		return '0', visual.RgbBlack, visual.RgbCursorNormal
	}
	rel = max(rel, -rel)
	ch := ' '
	switch {
	case rel%10 == 0:
		ch = rune('0' + rel/10%10)
	case rel%minorEvery == 0:
		ch = minor
	}
	return ch, visual.RgbIndicator, visual.RgbBackground
}
