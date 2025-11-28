package renderers

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// PingGridRenderer draws cursor row/column highlights and optional grid lines.
type PingGridRenderer struct {
	gameCtx *engine.GameContext
}

// NewPingGridRenderer creates a new ping grid renderer.
func NewPingGridRenderer(gameCtx *engine.GameContext) *PingGridRenderer {
	return &PingGridRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws the ping highlights and grid.
func (p *PingGridRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	defaultStyle := tcell.StyleDefault.Background(render.RgbBackground)

	// Get ping color based on mode
	pingColor := p.getPingColor()
	pingStyle := defaultStyle.Background(pingColor)

	// Draw row and column highlights
	p.drawPingHighlights(ctx, buf, pingStyle, pingColor)

	// Draw grid lines if ping is active
	if p.gameCtx.GetPingActive() {
		p.drawPingGrid(ctx, buf, pingStyle)
	}
}

// getPingColor determines the ping highlight color based on game mode.
func (p *PingGridRenderer) getPingColor() tcell.Color {
	// INSERT mode: use whitespace color (dark gray)
	// NORMAL/SEARCH mode: use character color (almost black)
	if p.gameCtx.IsInsertMode() {
		return render.RgbPingHighlight // Dark gray (50,50,50)
	}
	return render.RgbPingNormal // Almost black for NORMAL and SEARCH modes
}

// drawPingHighlights draws the cursor row and column highlights.
// Draws ONLY on cells with default/black background to avoid overwriting shield.
func (p *PingGridRenderer) drawPingHighlights(ctx render.RenderContext, buf *render.RenderBuffer, pingStyle tcell.Style, pingColor tcell.Color) {
	// Helper to draw ping only if cell has default background
	drawPingCell := func(x, y int) {
		cell := buf.Get(x, y)
		_, bg, _ := cell.Style.Decompose()
		// Only draw ping if background is default/black (don't overwrite shield)
		if bg == tcell.ColorDefault || bg == render.RgbBackground {
			buf.Set(x, y, ' ', pingStyle)
		}
	}

	// Highlight the row
	for x := 0; x < ctx.GameWidth; x++ {
		screenX := ctx.GameX + x
		screenY := ctx.GameY + ctx.CursorY
		if screenX >= ctx.GameX && screenX < ctx.Width && screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			drawPingCell(screenX, screenY)
		}
	}

	// Highlight the column
	for y := 0; y < ctx.GameHeight; y++ {
		screenX := ctx.GameX + ctx.CursorX
		screenY := ctx.GameY + y
		if screenX >= ctx.GameX && screenX < ctx.Width && screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			drawPingCell(screenX, screenY)
		}
	}
}

// drawPingGrid draws coordinate grid lines at 5-column intervals.
// Only draws on cells with default background.
func (p *PingGridRenderer) drawPingGrid(ctx render.RenderContext, buf *render.RenderBuffer, pingStyle tcell.Style) {
	// Helper to draw ping only if cell has default background
	drawPingCell := func(screenX, screenY int) {
		cell := buf.Get(screenX, screenY)
		_, bg, _ := cell.Style.Decompose()
		if bg == tcell.ColorDefault || bg == render.RgbBackground {
			buf.Set(screenX, screenY, ' ', pingStyle)
		}
	}

	// Vertical lines
	for n := 1; ; n++ {
		offset := 5 * n
		col := ctx.CursorX + offset
		if col >= ctx.GameWidth && ctx.CursorX-offset < 0 {
			break
		}
		if col < ctx.GameWidth {
			for y := 0; y < ctx.GameHeight; y++ {
				screenX := ctx.GameX + col
				screenY := ctx.GameY + y
				if screenX >= ctx.GameX && screenX < ctx.Width && screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
		col = ctx.CursorX - offset
		if col >= 0 {
			for y := 0; y < ctx.GameHeight; y++ {
				screenX := ctx.GameX + col
				screenY := ctx.GameY + y
				if screenX >= ctx.GameX && screenX < ctx.Width && screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
	}

	// Horizontal lines
	for n := 1; ; n++ {
		offset := 5 * n
		row := ctx.CursorY + offset
		if row >= ctx.GameHeight && ctx.CursorY-offset < 0 {
			break
		}
		if row < ctx.GameHeight {
			for x := 0; x < ctx.GameWidth; x++ {
				screenX := ctx.GameX + x
				screenY := ctx.GameY + row
				if screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
		row = ctx.CursorY - offset
		if row >= 0 {
			for x := 0; x < ctx.GameWidth; x++ {
				screenX := ctx.GameX + x
				screenY := ctx.GameY + row
				if screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
					drawPingCell(screenX, screenY)
				}
			}
		}
	}
}
