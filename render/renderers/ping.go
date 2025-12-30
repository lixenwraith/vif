// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// PingRenderer draws cursor row/column highlights and optional grid lines
type PingRenderer struct {
	gameCtx     *engine.GameContext
	pingStore   *engine.Store[component.PingComponent]
	shieldStore *engine.Store[component.ShieldComponent]

	// Bitmask for shield exclusion (1 bit per cell)
	// Reused across frames to avoid allocation
	exclusionMask []uint64
	maskWidth     int
	maskHeight    int
}

// NewPingRenderer creates a new ping renderer
func NewPingRenderer(gameCtx *engine.GameContext) *PingRenderer {
	return &PingRenderer{
		gameCtx:     gameCtx,
		pingStore:   engine.GetStore[component.PingComponent](gameCtx.World),
		shieldStore: engine.GetStore[component.ShieldComponent](gameCtx.World),
	}
}

// Render draws the ping highlights and grid
func (r *PingRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Get PingComponent from cursor (Single player assumption: ID 1/CursorEntity)
	ping, ok := r.pingStore.Get(r.gameCtx.CursorEntity)
	if !ok {
		return
	}

	// Early exit if nothing to draw
	if !ping.ShowCrosshair && !ping.GridActive {
		return
	}

	buf.SetWriteMask(constant.MaskPing)

	// 1. Compute Shield Exclusion Mask
	r.computeExclusionMask(r.gameCtx.World, ctx.GameWidth, ctx.GameHeight)

	// 2. Draw Crosshair (Row/Column Highlights)
	if ping.ShowCrosshair {
		var lineColor render.RGB
		if r.gameCtx.IsInsertMode() {
			lineColor = render.RgbPingHighlight
		} else {
			lineColor = render.RgbPingLineNormal
		}
		r.drawCrosshair(ctx, buf, lineColor)
	}

	// 3. Draw Grid Lines
	if ping.GridActive {
		r.drawGrid(ctx, buf, render.RgbPingGridNormal)
	}
}

// computeExclusionMask builds a 1-bit mask of all active shields
// O(Shields * ShieldArea), usually very small
func (r *PingRenderer) computeExclusionMask(world *engine.World, w, h int) {
	// Resize mask if dimensions changed
	// Need (w*h + 63) / 64 uint64s
	needed := (w*h + 63) / 64
	if len(r.exclusionMask) < needed || r.maskWidth != w || r.maskHeight != h {
		r.exclusionMask = make([]uint64, needed)
		r.maskWidth = w
		r.maskHeight = h
	} else {
		// Zero out existing mask
		for i := range r.exclusionMask {
			r.exclusionMask[i] = 0
		}
	}

	// Rasterize all active shields into the mask
	shields := r.shieldStore.All()
	for _, entity := range shields {
		shield, okS := r.shieldStore.Get(entity)
		pos, okP := world.Positions.Get(entity)
		if !okS || !okP || !shield.Active {
			continue
		}

		// Bounding box from Q16.16 radii
		rx := vmath.ToInt(shield.RadiusX)
		ry := vmath.ToInt(shield.RadiusY)
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

		for y := startY; y <= endY; y++ {
			dy := vmath.FromInt(y - pos.Y)
			dySq := vmath.Mul(dy, dy)
			rowOffset := y * w

			for x := startX; x <= endX; x++ {
				dx := vmath.FromInt(x - pos.X)
				dxSq := vmath.Mul(dx, dx)

				// Ellipse containment: (dx²*invRxSq + dy²*invRySq) <= 1.0
				if (vmath.Mul(dxSq, shield.InvRxSq) + vmath.Mul(dySq, shield.InvRySq)) <= vmath.Scale {
					idx := rowOffset + x
					r.exclusionMask[idx/64] |= (1 << (idx % 64))
				}
			}
		}
	}
}

// isExcluded checks if a cell is inside a shield
func (r *PingRenderer) isExcluded(x, y int) bool {
	if x < 0 || x >= r.maskWidth || y < 0 || y >= r.maskHeight {
		return false
	}
	idx := y*r.maskWidth + x
	return (r.exclusionMask[idx/64] & (1 << (idx % 64))) != 0
}

// drawCrosshair draws the crosshair lines respecting shield exclusion
func (r *PingRenderer) drawCrosshair(ctx render.RenderContext, buf *render.RenderBuffer, color render.RGB) {

	// Row
	screenY := ctx.GameY + ctx.CursorY
	if screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
		for x := 0; x < ctx.GameWidth; x++ {
			if !r.isExcluded(x, ctx.CursorY) {
				buf.Set(ctx.GameX+x, screenY, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}

	// Column
	screenX := ctx.GameX + ctx.CursorX
	if screenX >= ctx.GameX && screenX < ctx.GameX+ctx.GameWidth {
		for y := 0; y < ctx.GameHeight; y++ {
			if !r.isExcluded(ctx.CursorX, y) {
				buf.Set(screenX, ctx.GameY+y, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}
}

// drawGrid draws the 5-cell grid respecting shield exclusion
func (r *PingRenderer) drawGrid(ctx render.RenderContext, buf *render.RenderBuffer, color render.RGB) {
	// Vertical lines at ±5, ±10, etc.
	for n := 1; ; n++ {
		offset := 5 * n
		colRight := ctx.CursorX + offset
		colLeft := ctx.CursorX - offset
		inBounds := false

		if colRight < ctx.GameWidth {
			inBounds = true
			for y := 0; y < ctx.GameHeight; y++ {
				if !r.isExcluded(colRight, y) {
					buf.Set(ctx.GameX+colRight, ctx.GameY+y, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if colLeft >= 0 {
			inBounds = true
			for y := 0; y < ctx.GameHeight; y++ {
				if !r.isExcluded(colLeft, y) {
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
				if !r.isExcluded(x, rowDown) {
					buf.Set(ctx.GameX+x, ctx.GameY+rowDown, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if rowUp >= 0 {
			inBounds = true
			for x := 0; x < ctx.GameWidth; x++ {
				if !r.isExcluded(x, rowUp) {
					buf.Set(ctx.GameX+x, ctx.GameY+rowUp, ' ', render.DefaultBgRGB, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}

		if !inBounds {
			break
		}
	}
}