package renderers

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// PingGridRenderer draws cursor row/column highlights and optional grid lines
type PingGridRenderer struct {
	gameCtx *engine.GameContext
}

// NewPingGridRenderer creates a new ping grid renderer
func NewPingGridRenderer(gameCtx *engine.GameContext) *PingGridRenderer {
	return &PingGridRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws the ping highlights and grid
func (p *PingGridRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	// Get ping color based on mode
	pingColor := p.getPingColor()

	// Draw row and column highlights
	p.drawPingHighlights(ctx, buf, pingColor)

	// Draw grid lines if ping is active
	if p.gameCtx.GetPingActive() {
		p.drawPingGrid(ctx, buf, pingColor)
	}
}

// getPingColor determines the ping highlight color based on game mode
func (p *PingGridRenderer) getPingColor() render.RGB {
	if p.gameCtx.IsInsertMode() {
		return render.RgbPingHighlight
	}
	return render.RgbPingNormal
}

// drawPingHighlights - write-only, no buf.Get
// Grid is layer 100, renders first. Just emit all highlights unconditionally.
// Higher layers (shields at 300) will blend over as needed.
func (p *PingGridRenderer) drawPingHighlights(ctx render.RenderContext, buf *render.RenderBuffer, pingColor render.RGB) {
	// Highlight the row
	for x := 0; x < ctx.GameWidth; x++ {
		screenX := ctx.GameX + x
		screenY := ctx.GameY + ctx.CursorY
		if screenX >= ctx.GameX && screenX < ctx.Width &&
			screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			buf.Set(screenX, screenY, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
		}
	}

	// Highlight the column
	for y := 0; y < ctx.GameHeight; y++ {
		screenX := ctx.GameX + ctx.CursorX
		screenY := ctx.GameY + y
		if screenX >= ctx.GameX && screenX < ctx.Width &&
			screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			buf.Set(screenX, screenY, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
		}
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals
// Only draws on cells with default background
func (p *PingGridRenderer) drawPingGrid(ctx render.RenderContext, buf *render.RenderBuffer, pingColor render.RGB) {
	// Vertical lines at ±5, ±10, ±15, etc from cursor
	for n := 1; ; n++ {
		offset := 5 * n
		colRight := ctx.CursorX + offset
		colLeft := ctx.CursorX - offset

		if colRight >= ctx.GameWidth && colLeft < 0 {
			break
		}

		if colRight < ctx.GameWidth {
			for y := 0; y < ctx.GameHeight; y++ {
				buf.Set(ctx.GameX+colRight, ctx.GameY+y, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
			}
		}

		if colLeft >= 0 {
			for y := 0; y < ctx.GameHeight; y++ {
				buf.Set(ctx.GameX+colLeft, ctx.GameY+y, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
			}
		}
	}

	// Horizontal lines at ±5, ±10, ±15, etc from cursor
	for n := 1; ; n++ {
		offset := 5 * n
		rowDown := ctx.CursorY + offset
		rowUp := ctx.CursorY - offset

		if rowDown >= ctx.GameHeight && rowUp < 0 {
			break
		}

		if rowDown < ctx.GameHeight {
			for x := 0; x < ctx.GameWidth; x++ {
				buf.Set(ctx.GameX+x, ctx.GameY+rowDown, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
			}
		}

		if rowUp >= 0 {
			for x := 0; x < ctx.GameWidth; x++ {
				buf.Set(ctx.GameX+x, ctx.GameY+rowUp, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, tcell.AttrNone)
			}
		}
	}
}