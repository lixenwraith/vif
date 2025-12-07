package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	buf.SetWriteMask(render.MaskGrid)

	// Draw row and column highlights with line color
	lineColor := p.getPingLineColor()
	p.drawPingHighlights(ctx, buf, lineColor)

	// Draw grid lines if ping is active (NORMAL mode only, uses grid color)
	if p.gameCtx.GetPingActive() {
		gridColor := p.getPingGridColor()
		p.drawPingGrid(ctx, buf, gridColor)
	}
}

// getPingLineColor returns color for cursor row/column highlights
func (p *PingGridRenderer) getPingLineColor() render.RGB {
	if p.gameCtx.IsInsertMode() {
		return render.RgbPingHighlight // Almost black (5,5,5)
	}
	return render.RgbPingLineNormal // Dark gray (40,40,40)
}

// getPingGridColor returns color for 5-interval grid lines
func (p *PingGridRenderer) getPingGridColor() render.RGB {
	return render.RgbPingGridNormal // Slightly lighter gray (55,55,55)
}

// drawPingHighlights - write-only, no buf.Get
// Grid is low layer, renders first, emit all highlights unconditionally, higher layers will blend as needed
func (p *PingGridRenderer) drawPingHighlights(ctx render.RenderContext, buf *render.RenderBuffer, pingColor render.RGB) {
	// Check if shield is active on cursor to create exclusion zone
	shieldExclusion := false
	var shieldCenterX, shieldCenterY float64
	var invRxSq, invRySq float64

	if p.gameCtx.State.GetShieldActive() {
		if shield, ok := p.gameCtx.World.Shields.Get(p.gameCtx.CursorEntity); ok {
			shieldExclusion = true
			pos, _ := p.gameCtx.World.Positions.Get(p.gameCtx.CursorEntity)
			shieldCenterX = float64(pos.X)
			shieldCenterY = float64(pos.Y)
			// Precompute inverse radii squared
			if shield.RadiusX > 0 && shield.RadiusY > 0 {
				invRxSq = 1.0 / (shield.RadiusX * shield.RadiusX)
				invRySq = 1.0 / (shield.RadiusY * shield.RadiusY)
			} else {
				shieldExclusion = false
			}
		}
	}

	// TODO: is this optimized?
	// Helper to check exclusion
	isExcluded := func(x, y int) bool {
		if !shieldExclusion {
			return false
		}
		dx := float64(x) - shieldCenterX
		dy := float64(y) - shieldCenterY
		// Ellipse equation: x^2/a^2 + y^2/b^2 <= 1
		return (dx*dx*invRxSq + dy*dy*invRySq) <= 1.0
	}

	// Highlight the row
	for x := 0; x < ctx.GameWidth; x++ {
		// Exclusion check: Don't draw line inside shield
		if isExcluded(x, ctx.CursorY) {
			continue
		}

		screenX := ctx.GameX + x
		screenY := ctx.GameY + ctx.CursorY
		if screenX >= ctx.GameX && screenX < ctx.Width &&
			screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			buf.Set(screenX, screenY, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
		}
	}

	// Highlight the column
	for y := 0; y < ctx.GameHeight; y++ {
		// Exclusion check
		if isExcluded(ctx.CursorX, y) {
			continue
		}

		screenX := ctx.GameX + ctx.CursorX
		screenY := ctx.GameY + y
		if screenX >= ctx.GameX && screenX < ctx.Width &&
			screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			buf.Set(screenX, screenY, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
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
				buf.Set(ctx.GameX+colRight, ctx.GameY+y, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}

		if colLeft >= 0 {
			for y := 0; y < ctx.GameHeight; y++ {
				buf.Set(ctx.GameX+colLeft, ctx.GameY+y, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
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
				buf.Set(ctx.GameX+x, ctx.GameY+rowDown, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}

		if rowUp >= 0 {
			for x := 0; x < ctx.GameWidth; x++ {
				buf.Set(ctx.GameX+x, ctx.GameY+rowUp, ' ', render.DefaultBgRGB, pingColor, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}
}