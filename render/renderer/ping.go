package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
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
func (r *PingRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Get PingComponent from cursor (Single player assumption: ID 1/CursorEntity)
	pingComp, ok := r.gameCtx.World.Components.Ping.GetComponent(r.gameCtx.World.Resources.Player.Entity)
	if !ok {
		return
	}

	// Early exit if nothing to draw
	if !pingComp.ShowCrosshair && !pingComp.GridActive {
		return
	}

	buf.SetWriteMask(visual.MaskPing)

	// 1. Compute shield exclusion mask sized to viewport
	r.computeExclusionMask(r.gameCtx.World, ctx)
	// Get cursor position in viewport coordinates
	cursorVX, cursorVY := ctx.CursorViewportPos()

	// 2. Draw Crosshair (Row/Column Highlights)
	if pingComp.ShowCrosshair {
		var lineColor terminal.RGB
		if r.gameCtx.IsInsertMode() {
			lineColor = visual.RgbPingHighlight
		} else {
			lineColor = visual.RgbPingLineNormal
		}
		r.drawCrosshair(ctx, buf, cursorVX, cursorVY, lineColor)
	}

	// 3. Draw Grid Lines
	if pingComp.GridActive {
		r.drawGrid(ctx, buf, cursorVX, cursorVY, visual.RgbPingGridNormal)
	}
}

// computeExclusionMask builds a 1-bit mask of all active shields in viewport space
// O(Shields * ShieldArea), usually very small
func (r *PingRenderer) computeExclusionMask(world *engine.World, ctx render.RenderContext) {
	w, h := ctx.ViewportWidth, ctx.ViewportHeight

	// Resize mask if dimensions changed
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
	shieldEntities := r.gameCtx.World.Components.Shield.GetAllEntities()
	for _, shieldEntity := range shieldEntities {
		shieldComp, ok := r.gameCtx.World.Components.Shield.GetComponent(shieldEntity)
		if !ok || !shieldComp.Active {
			continue
		}

		shieldPos, ok := world.Positions.GetPosition(shieldEntity)
		if !ok {
			continue
		}

		// Shield center in viewport coords
		shieldVX, shieldVY, shieldVisible := ctx.MapToViewport(shieldPos.X, shieldPos.Y)
		if !shieldVisible {
			// Shield center off-screen, but edges might be visible
			// For simplicity, skip entirely; can refine later if needed
			continue
		}

		// Bounding box
		rx := vmath.ToInt(shieldComp.RadiusX)
		ry := vmath.ToInt(shieldComp.RadiusY)

		// Bounding box in viewport coords
		startX := shieldVX - rx
		endX := shieldVX + rx
		startY := shieldVY - ry
		endY := shieldVY + ry

		// Clamp to viewport
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
			dy := vmath.FromInt(y - shieldVY)
			dySq := vmath.Mul(dy, dy)
			rowOffset := y * w

			for x := startX; x <= endX; x++ {
				dx := vmath.FromInt(x - shieldVX)
				dxSq := vmath.Mul(dx, dx)

				// Ellipse containment: (dx²*invRxSq + dy²*invRySq) <= 1.0
				if (vmath.Mul(dxSq, shieldComp.InvRxSq) + vmath.Mul(dySq, shieldComp.InvRySq)) <= vmath.Scale {
					idx := rowOffset + x
					r.exclusionMask[idx/64] |= (1 << (idx % 64))
				}
			}
		}
	}
}

// isExcluded checks if a viewport coordinate is inside a shield
func (r *PingRenderer) isExcluded(vx, vy int) bool {
	if vx < 0 || vx >= r.maskWidth || vy < 0 || vy >= r.maskHeight {
		return false
	}
	idx := vy*r.maskWidth + vx
	return (r.exclusionMask[idx/64] & (1 << (idx % 64))) != 0
}

// drawCrosshair draws the crosshair lines in viewport space
func (r *PingRenderer) drawCrosshair(ctx render.RenderContext, buf *render.RenderBuffer, cursorVX, cursorVY int, color terminal.RGB) {
	pingBounds := r.gameCtx.World.GetPingAbsoluteBounds()

	// Convert ping bounds from map coords to viewport coords
	minVX, minVY, _ := ctx.MapToViewport(pingBounds.MinX, pingBounds.MinY)
	maxVX, maxVY, _ := ctx.MapToViewport(pingBounds.MaxX, pingBounds.MaxY)

	// Clamp to viewport
	if minVX < 0 {
		minVX = 0
	}
	if minVY < 0 {
		minVY = 0
	}
	if maxVX >= ctx.ViewportWidth {
		maxVX = ctx.ViewportWidth - 1
	}
	if maxVY >= ctx.ViewportHeight {
		maxVY = ctx.ViewportHeight - 1
	}

	// Draw horizontal band (rows from minVY to maxVY, full viewport width)
	for vy := minVY; vy <= maxVY; vy++ {
		screenY := ctx.GameYOffset + vy
		for vx := 0; vx < ctx.ViewportWidth; vx++ {
			if !r.isExcluded(vx, vy) {
				buf.Set(ctx.GameXOffset+vx, screenY, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}

	// Draw vertical band (columns from minVX to maxVX, full viewport height)
	for vx := minVX; vx <= maxVX; vx++ {
		screenX := ctx.GameXOffset + vx
		for vy := 0; vy < ctx.ViewportHeight; vy++ {
			// Skip cells already drawn by horizontal band
			if vy >= minVY && vy <= maxVY {
				continue
			}
			if !r.isExcluded(vx, vy) {
				buf.Set(screenX, ctx.GameYOffset+vy, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}
}

// drawGrid draws the 5-cell grid in viewport space
func (r *PingRenderer) drawGrid(ctx render.RenderContext, buf *render.RenderBuffer, cursorVX, cursorVY int, color terminal.RGB) {
	// Vertical lines at ±5, ±10, etc. from cursor
	for n := 1; ; n++ {
		offset := 5 * n
		colRight := cursorVX + offset
		colLeft := cursorVX - offset
		inBounds := false

		if colRight < ctx.ViewportWidth {
			inBounds = true
			for vy := 0; vy < ctx.ViewportHeight; vy++ {
				if !r.isExcluded(colRight, vy) {
					buf.Set(ctx.GameXOffset+colRight, ctx.GameYOffset+vy, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if colLeft >= 0 {
			inBounds = true
			for vy := 0; vy < ctx.ViewportHeight; vy++ {
				if !r.isExcluded(colLeft, vy) {
					buf.Set(ctx.GameXOffset+colLeft, ctx.GameYOffset+vy, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}

		if !inBounds {
			break
		}
	}

	// Horizontal lines at ±5, ±10, etc. from cursor
	for n := 1; ; n++ {
		offset := 5 * n
		rowDown := cursorVY + offset
		rowUp := cursorVY - offset
		inBounds := false

		if rowDown < ctx.ViewportHeight {
			inBounds = true
			for vx := 0; vx < ctx.ViewportWidth; vx++ {
				if !r.isExcluded(vx, rowDown) {
					buf.Set(ctx.GameXOffset+vx, ctx.GameYOffset+rowDown, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}
		if rowUp >= 0 {
			inBounds = true
			for vx := 0; vx < ctx.ViewportWidth; vx++ {
				if !r.isExcluded(vx, rowUp) {
					buf.Set(ctx.GameXOffset+vx, ctx.GameYOffset+rowUp, ' ', visual.RgbBackground, color, render.BlendReplace, 1.0, terminal.AttrNone)
				}
			}
		}

		if !inBounds {
			break
		}
	}
}