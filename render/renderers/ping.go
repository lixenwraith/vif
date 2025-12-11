// @focus: #render { scene } #vfx { ping }
package renderers

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// PingRenderer draws cursor row/column highlights and optional grid lines
type PingRenderer struct {
	gameCtx *engine.GameContext

	// Bitmask for shield exclusion (1 bit per cell)
	// Reused across frames to avoid allocation
	exclusionMask []uint64
	maskWidth     int
	maskHeight    int
}

// NewPingRenderer creates a new ping renderer
func NewPingRenderer(gameCtx *engine.GameContext) *PingRenderer {
	return &PingRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws the ping highlights and grid
func (p *PingRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	// Get PingComponent from cursor (Single player assumption: ID 1/CursorEntity)
	ping, ok := world.Pings.Get(p.gameCtx.CursorEntity)
	if !ok {
		return
	}

	// Early exit if nothing to draw
	if !ping.ShowCrosshair && !ping.GridActive {
		return
	}

	buf.SetWriteMask(render.MaskGrid)

	// 1. Compute Shield Exclusion Mask
	p.computeExclusionMask(world, ctx.GameWidth, ctx.GameHeight)

	// 2. Draw Crosshair (Row/Column Highlights)
	if ping.ShowCrosshair {
		lineColor := p.getPingLineColor(ping)
		p.drawCrosshair(ctx, buf, lineColor)
	}

	// 3. Draw Grid Lines
	if ping.GridActive {
		gridColor := p.getPingGridColor(ping)
		p.drawGrid(ctx, buf, gridColor)
	}
}

// computeExclusionMask builds a 1-bit mask of all active shields
// O(Shields * ShieldArea), usually very small
func (p *PingRenderer) computeExclusionMask(world *engine.World, w, h int) {
	// Resize mask if dimensions changed
	// Need (w*h + 63) / 64 uint64s
	needed := (w*h + 63) / 64
	if len(p.exclusionMask) < needed || p.maskWidth != w || p.maskHeight != h {
		p.exclusionMask = make([]uint64, needed)
		p.maskWidth = w
		p.maskHeight = h
	} else {
		// Zero out existing mask
		for i := range p.exclusionMask {
			p.exclusionMask[i] = 0
		}
	}

	// Rasterize all active shields into the mask
	shields := world.Shields.All()
	for _, entity := range shields {
		shield, okS := world.Shields.Get(entity)
		pos, okP := world.Positions.Get(entity)
		if !okS || !okP || !shield.Active {
			continue
		}

		// Simple bounding box for ellipse
		rx := int(shield.RadiusX)
		ry := int(shield.RadiusY)
		startX := pos.X - rx
		endX := pos.X + rx
		startY := pos.Y - ry
		endY := pos.Y + ry

		// Clamp
		if startX < 0 {
			startX = 0
		}
		if endX >= w {
			endX = w - 1
		}
		if startY < 0 {
			startY = 0
		}
		if endY >= h {
			endY = h - 1
		}

		// Ellipse calculation constants
		invRxSq := 1.0 / (shield.RadiusX * shield.RadiusX)
		invRySq := 1.0 / (shield.RadiusY * shield.RadiusY)

		for y := startY; y <= endY; y++ {
			dy := float64(y - pos.Y)
			rowOffset := y * w

			for x := startX; x <= endX; x++ {
				dx := float64(x - pos.X)
				// Check ellipse containment
				if (dx*dx*invRxSq + dy*dy*invRySq) <= 1.0 {
					// Set bit
					idx := rowOffset + x
					p.exclusionMask[idx/64] |= (1 << (idx % 64))
				}
			}
		}
	}
}

// isExcluded checks if a cell is inside a shield
func (p *PingRenderer) isExcluded(x, y int) bool {
	if x < 0 || x >= p.maskWidth || y < 0 || y >= p.maskHeight {
		return false
	}
	idx := y*p.maskWidth + x
	return (p.exclusionMask[idx/64] & (1 << (idx % 64))) != 0
}

func (p *PingRenderer) getPingLineColor(ping components.PingComponent) render.RGB {
	if ping.CrosshairColor != components.ColorNone {
		// Use component override
		if ping.CrosshairColor == components.ColorNormal && p.gameCtx.IsInsertMode() {
			return render.RgbPingHighlight
		}
		// TODO: Map other ColorClasses if needed
	}
	// Default
	if p.gameCtx.IsInsertMode() {
		return render.RgbPingHighlight
	}
	return render.RgbPingLineNormal
}

func (p *PingRenderer) getPingGridColor(ping components.PingComponent) render.RGB {
	if ping.GridColor != components.ColorNone {
		// Placeholder for mapping
	}
	return render.RgbPingGridNormal
}

// drawCrosshair draws the crosshair lines respecting shield exclusion
func (p *PingRenderer) drawCrosshair(ctx render.RenderContext, buf *render.RenderBuffer, color render.RGB) {

	// Row
	screenY := ctx.GameY + ctx.CursorY
	if screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
		for x := 0; x < ctx.GameWidth; x++ {
			if !p.isExcluded(x, ctx.CursorY) {
				buf.Set(ctx.GameX+x, screenY, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}

	// Column
	screenX := ctx.GameX + ctx.CursorX
	if screenX >= ctx.GameX && screenX < ctx.GameX+ctx.GameWidth {
		for y := 0; y < ctx.GameHeight; y++ {
			if !p.isExcluded(ctx.CursorX, y) {
				buf.Set(screenX, ctx.GameY+y, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}
}

// drawGrid draws the 5-cell grid respecting shield exclusion
func (p *PingRenderer) drawGrid(ctx render.RenderContext, buf *render.RenderBuffer, color render.RGB) {
	// Vertical lines at ±5, ±10, etc.
	for n := 1; ; n++ {
		offset := 5 * n
		colRight := ctx.CursorX + offset
		colLeft := ctx.CursorX - offset
		inBounds := false

		if colRight < ctx.GameWidth {
			inBounds = true
			for y := 0; y < ctx.GameHeight; y++ {
				if !p.isExcluded(colRight, y) {
					buf.Set(ctx.GameX+colRight, ctx.GameY+y, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if colLeft >= 0 {
			inBounds = true
			for y := 0; y < ctx.GameHeight; y++ {
				if !p.isExcluded(colLeft, y) {
					buf.Set(ctx.GameX+colLeft, ctx.GameY+y, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}

		if !inBounds {
			break
		}
	}

	// Horizontal lines
	for n := 1; ; n++ {
		offset := 5 * n
		rowDown := ctx.CursorY + offset
		rowUp := ctx.CursorY - offset
		inBounds := false

		if rowDown < ctx.GameHeight {
			inBounds = true
			for x := 0; x < ctx.GameWidth; x++ {
				if !p.isExcluded(x, rowDown) {
					buf.Set(ctx.GameX+x, ctx.GameY+rowDown, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if rowUp >= 0 {
			inBounds = true
			for x := 0; x < ctx.GameWidth; x++ {
				if !p.isExcluded(x, rowUp) {
					buf.Set(ctx.GameX+x, ctx.GameY+rowUp, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}

		if !inBounds {
			break
		}
	}
}