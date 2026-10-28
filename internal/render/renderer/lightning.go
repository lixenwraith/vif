package renderer

import (
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// lightningBoltRenderer defines the signature for mode-specific bolt rendering
// Called per lightning entity with accumulated path data
type lightningBoltRenderer func(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64)

// LightningRenderer draws transient energy bolts: quadrant characters at 2x2 sub-cell
// resolution in TrueColor, background-filled cells in 256 colors
type LightningRenderer struct {
	gameCtx *engine.GameContext

	// Mode-specific renderer selected at construction
	renderLightning lightningBoltRenderer

	// One bolt's path and the cells it crosses, reused so a frame allocates nothing
	points   []struct{ X, Y int }
	cellHits map[uint64]uint8
}

// NewLightningRenderer creates a new lightning renderer with mode-appropriate rendering path
func NewLightningRenderer(ctx *engine.GameContext) *LightningRenderer {
	r := &LightningRenderer{
		gameCtx:  ctx,
		cellHits: make(map[uint64]uint8),
	}

	if r.gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderLightning = r.renderLightning256
	} else {
		r.renderLightning = r.renderLightningTrueColor
	}

	return r
}

// Render draws all active lightning bolts using the mode-appropriate renderer
func (r *LightningRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	lightnings := r.gameCtx.World.Components.Lightning
	if lightnings.CountEntities() == 0 {
		return
	}

	// MaskNone keeps OcclusionDim off the background under a TrueColor bolt
	buf.SetWriteMask(visual.MaskNone)

	timers := r.gameCtx.World.Components.Timer
	lightnings.Each(func(e core.Entity, l *component.LightningComponent) bool {
		if timer, ok := timers.GetPtr(e); !ok || timer.Slots[component.TimerLifetime].Remaining <= 0 {
			return true
		}

		// Resolve origin position (map coords)
		originX, originY := l.OriginX, l.OriginY
		if l.OriginEntity != 0 {
			if pos, ok := r.gameCtx.World.Positions.GetPosition(l.OriginEntity); ok {
				originX, originY = pos.X, pos.Y
			}
		}

		// Resolve target position (map coords)
		targetX, targetY := l.TargetX, l.TargetY
		if l.TargetEntity != 0 {
			if pos, ok := r.gameCtx.World.Positions.GetPosition(l.TargetEntity); ok {
				targetX, targetY = pos.X, pos.Y
			}
		}

		// Deterministic path per PathSeed and AnimFrame
		rng := vmath.NewFastRand(vmath.MixChain(l.PathSeed, uint64(l.AnimFrame)))

		// Generate fractal path in sub-pixel coordinates (2x resolution)
		// Shared between both rendering modes for consistent path shape
		points := r.generateFractalPath(originX, originY, targetX, targetY, rng)

		// Dispatch to mode-specific renderer
		r.renderLightning(ctx, buf, points, l.ColorType, parameter.LightningAlpha)
		return true
	})
}

// generateFractalPath creates a jagged lightning path by midpoint displacement in
// quadrant space, two quadrants a cell each way, aimed from one end cell's centre to
// the other's. A centre is the corner its cell's four quadrants share, so each end
// is the quadrant of its cell nearest the path's next point: a bolt meets both cells
// on the side facing the other, from any direction. A bolt within one cell has none.
func (r *LightningRenderer) generateFractalPath(x1, y1, x2, y2 int, rng *vmath.FastRand) []struct{ X, Y int } {
	points := r.points[:0]
	if x1 == x2 && y1 == y2 {
		r.points = points
		return points
	}
	sx1, sy1 := float64(2*x1+1), float64(2*y1+1)
	dxF, dyF := float64(2*(x2-x1)), float64(2*(y2-y1))
	dist := vmath.Magnitude(dxF, dyF)

	// About one segment per 10 quadrants
	segments := min(max(int(dist/10.0), 4), 32)

	// Unit perpendicular the jitter displaces along
	perpX := -dyF / dist
	perpY := dxF / dist

	// Two octaves: one spine offset bends the whole path into a gentle arc, so
	// bolts do not bundle straight; per-segment detail adds the jag
	spineRand := rng.Next()
	spineOffset := float64(int64(spineRand>>32))/(1<<31) - 1.0
	spine := spineOffset * 4.0
	detailMagnitude := 6.0

	points = append(points, struct{ X, Y int }{}) // the origin's quadrant, once its neighbour is known
	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)
		bx := sx1 + dxF*t
		by := sy1 + dyF*t

		// Spine on a parabola, detail on a sine, both peaking mid-path; the detail's
		// floor keeps the points next to the ends dancing
		spineJitter := spine * 4.0 * t * (1.0 - t)
		envelope := max(vmath.Sin(t*vmath.TwoPi/2.0), 0.15)

		detailRand := rng.Next()
		detailFrac := float64(int64(detailRand>>32))/(1<<31) - 1.0
		detailJitter := detailFrac * detailMagnitude * envelope

		totalJitter := spineJitter + detailJitter
		points = append(points, struct{ X, Y int }(vmath.PointAt(bx+perpX*totalJitter, by+perpY*totalJitter)))
	}
	points = append(points, endQuadrant(x2, y2, points[len(points)-1]))
	points[0] = endQuadrant(x1, y1, points[1])
	r.points = points
	return points
}

// endQuadrant is the quadrant of cell (x, y) nearest the quadrant p
func endQuadrant(x, y int, p struct{ X, Y int }) struct{ X, Y int } {
	return struct{ X, Y int }{min(max(p.X, 2*x), 2*x+1), min(max(p.Y, 2*y), 2*y+1)}
}

// renderLightningTrueColor draws lightning using quadrant block characters with screen blending
// Provides full 2x2 sub-pixel resolution with smooth color gradients
func (r *LightningRenderer) renderLightningTrueColor(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64) {

	c := visual.LightningTrueColorLUT[colorType][0]
	for key, bits := range r.traceBolt(points) {
		screenX, screenY, visible := ctx.MapToScreen(int(int32(key>>32)), int(int32(key)))
		if !visible {
			continue
		}
		char := visual.QuadrantChars[bits]
		if char == ' ' {
			continue
		}

		// Screen blend foreground only - background untouched for theme preservation
		buf.Set(screenX, screenY, char, c, visual.RgbBlack, render.BlendScreenFg, alpha, terminal.AttrNone)
	}
}

// traceBolt gathers the quadrants a path crosses by Bresenham, per cell. Each end cell
// keeps only its end quadrant, so a short bolt that wanders back over the cell it
// leaves or strikes passes behind it rather than across it.
func (r *LightningRenderer) traceBolt(points []struct{ X, Y int }) map[uint64]uint8 {
	hits := r.cellHits
	clear(hits)
	if len(points) == 0 {
		return hits
	}
	for i := range len(points) - 1 {
		for line := vmath.NewLine(points[i].X, points[i].Y, points[i+1].X, points[i+1].Y); ; line = line.Next() {
			key, bit := quadrantHit(line.X(), line.Y())
			hits[key] |= bit
			if line.AtEnd() {
				break
			}
		}
	}
	for _, end := range [2]struct{ X, Y int }{points[0], points[len(points)-1]} {
		key, bit := quadrantHit(end.X, end.Y)
		hits[key] = bit
	}
	return hits
}

// quadrantHit is a quadrant's cell, its coordinates the key's 32-bit halves, and its
// bit in the cell's bitmap (bit0=UL, bit1=UR, bit2=LL, bit3=LR). A shift, not a
// division, keeps a quadrant past the map's top or left edge in its own cell.
func quadrantHit(qx, qy int) (uint64, uint8) {
	return uint64(uint32(qx>>1))<<32 | uint64(uint32(qy>>1)), uint8(1 << ((qy&1)*2 + qx&1))
}

// renderLightning256 fills the background of every cell the bolt crosses: console
// fonts lack the half blocks, and the text under the bolt stays readable
func (r *LightningRenderer) renderLightning256(ctx render.RenderContext, buf *render.RenderBuffer,
	points []struct{ X, Y int }, colorType component.LightningColorType, alpha float64) {
	// No alpha in 256 colors: a bolt is drawn until it has nearly faded
	if alpha < 0.1 {
		return
	}
	paletteIdx := visual.Lightning256ColorLUT[colorType]
	for key := range r.traceBolt(points) {
		screenX, screenY, visible := ctx.MapToScreen(int(int32(key>>32)), int(int32(key)))
		if visible {
			buf.SetBg256(screenX, screenY, paletteIdx)
		}
	}
}
