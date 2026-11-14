package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// PingRenderer draws the local cursor's crosshair: the rows and columns its ping
// bounds cover, across the map
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

// Render draws the crosshair, sparing the cells a shield covers
func (r *PingRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pingComp, ok := r.gameCtx.World.Components.Ping.GetPtr(r.gameCtx.World.Resources.Player.Entity)
	if !ok || !pingComp.ShowCrosshair {
		return
	}

	buf.SetWriteMask(visual.MaskPing)
	r.computeExclusionMask(r.gameCtx.World, ctx)

	lineColor := visual.RgbPingLineNormal
	if r.gameCtx.IsInsertMode() {
		lineColor = visual.RgbPingHighlight
	}
	r.drawCrosshair(ctx, buf, lineColor)
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
	shields := world.Components.Shield
	if shields.CountEntities() == 0 {
		return
	}
	shields.Each(func(shieldEntity core.Entity, shieldComp *component.ShieldComponent) bool {
		if !shieldComp.Active || !(shieldComp.Radius > 0) {
			return true
		}

		// Rasterized where the shield is drawn, which for this instance's own cursor
		// is the D-18 predicted cell (see ShieldRenderer).
		shieldPos, ok := world.CursorCell(shieldEntity)
		if !ok {
			return true
		}

		// Shield center in viewport coords
		shieldVX, shieldVY, shieldVisible := ctx.MapToViewport(shieldPos.X, shieldPos.Y)
		if !shieldVisible {
			// Shield center off-screen, but edges might be visible
			// For simplicity, skip entirely; can refine later if needed
			return true
		}

		rx, ry := world.ShieldReach(shieldComp)
		radiusSq := shieldComp.Radius * shieldComp.Radius

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
			dyCirc := ctx.CellAspect.Circular(float64(y - shieldVY))
			rowOffset := y * w

			for x := startX; x <= endX; x++ {
				if vmath.CircleContains(float64(x-shieldVX), dyCirc, radiusSq) {
					idx := rowOffset + x
					r.exclusionMask[idx/64] |= (1 << (idx % 64))
				}
			}
		}
		return true
	})
}

// isExcluded checks if a viewport coordinate is inside a shield
func (r *PingRenderer) isExcluded(vx, vy int) bool {
	if vx < 0 || vx >= r.maskWidth || vy < 0 || vy >= r.maskHeight {
		return false
	}
	idx := vy*r.maskWidth + vx
	return (r.exclusionMask[idx/64] & (1 << (idx % 64))) != 0
}

// drawCrosshair draws the crosshair lines in viewport space, spanning the map
// rather than the viewport: on a terminal wider than the map the two differ, and
// a line crossing the centring margin is a line drawn outside the world
func (r *PingRenderer) drawCrosshair(ctx render.RenderContext, buf *render.RenderBuffer, c color.RGB) {
	pf := ctx.PlayfieldViewportRect()
	if pf.Empty() {
		return
	}
	pingBounds := r.gameCtx.World.GetPingAbsoluteBounds()

	// Convert ping bounds from map coords to viewport coords
	minVX, minVY, _ := ctx.MapToViewport(pingBounds.MinX, pingBounds.MinY)
	maxVX, maxVY, _ := ctx.MapToViewport(pingBounds.MaxX, pingBounds.MaxY)

	// Clamp to the visible map
	minVX = max(minVX, pf.X0)
	minVY = max(minVY, pf.Y0)
	maxVX = min(maxVX, pf.X1-1)
	maxVY = min(maxVY, pf.Y1-1)

	// Draw horizontal band (rows from minVY to maxVY, full map width)
	for vy := minVY; vy <= maxVY; vy++ {
		screenY := ctx.GameYOffset + vy
		for vx := pf.X0; vx < pf.X1; vx++ {
			if !r.isExcluded(vx, vy) {
				buf.Set(ctx.GameXOffset+vx, screenY, ' ', visual.RgbBackground, c, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}

	// Draw vertical band (columns from minVX to maxVX, full map height)
	for vx := minVX; vx <= maxVX; vx++ {
		screenX := ctx.GameXOffset + vx
		for vy := pf.Y0; vy < pf.Y1; vy++ {
			// Skip cells already drawn by horizontal band
			if vy >= minVY && vy <= maxVY {
				continue
			}
			if !r.isExcluded(vx, vy) {
				buf.Set(screenX, ctx.GameYOffset+vy, ' ', visual.RgbBackground, c, render.BlendReplace, 1.0, terminal.AttrNone)
			}
		}
	}
}
