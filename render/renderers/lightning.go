package renderers

import (
	"math"
	"math/rand"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// LightningRenderer draws transient energy beams
type LightningRenderer struct {
	gameCtx        *engine.GameContext
	lightningStore *engine.Store[component.LightningComponent]
}

// NewLightningRenderer creates a new lightning renderer
func NewLightningRenderer(ctx *engine.GameContext) *LightningRenderer {
	return &LightningRenderer{
		gameCtx:        ctx,
		lightningStore: engine.GetStore[component.LightningComponent](ctx.World),
	}
}

// Render draws lightning bolts using additive blending
func (r *LightningRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.lightningStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	// Sizzle: Use frame number to seed RNG for rapid, electric vibration (60hz)
	frameSeed := ctx.FrameNumber

	for _, e := range entities {
		l, ok := r.lightningStore.Get(e)
		if !ok || l.Duration <= 0 {
			continue
		}

		// Calculate life progress
		lifeRatio := float64(l.Remaining) / float64(l.Duration)
		if lifeRatio <= 0 {
			continue
		}

		// Cap max alpha to prevent "blown out" white blobs when multiple bolts overlap
		// Range: 0.0 -> 0.8 -> 0.0
		alpha := lifeRatio
		if alpha > 0.8 {
			alpha = 0.8
		}

		// Color: Cool Cyan/Blue core -> Hot White center
		color := render.Lerp(render.RgbDrain, render.RgbEnergyBlinkWhite, lifeRatio)

		// Deterministic RNG seeded by EntityID + FrameNumber
		// Ensures all bolts vibrate independently and update every frame
		seed := int64(e)*7919 + frameSeed
		rng := rand.New(rand.NewSource(seed))

		// Generate fractal path with distance-weighted jitter
		points := r.generateFractalPath(l.OriginX, l.OriginY, l.TargetX, l.TargetY, rng)

		// Draw path
		for i := 0; i < len(points)-1; i++ {
			p1 := points[i]
			p2 := points[i+1]
			r.drawLine(ctx, buf, p1.X, p1.Y, p2.X, p2.Y, color, alpha)
		}
	}
}

// generateFractalPath creates a jagged path using midpoint displacement
func (r *LightningRenderer) generateFractalPath(x1, y1, x2, y2 int, rng *rand.Rand) []struct{ X, Y int } {
	dx := x2 - x1
	dy := y2 - y1
	distSq := float64(dx*dx + dy*dy)
	dist := math.Sqrt(distSq)

	if dist < 1.0 {
		dist = 1.0
	}

	// Dynamic segment count: ~1 segment every 4 cells
	// Increased density for smoother "electric" look
	segments := int(dist / 4.0)
	if segments < 2 {
		segments = 2
	}

	// Jitter Calculation
	// "Shorter distances vibrate in a larger range"
	// Base jitter (3.0) ensures short lines separate visually
	// Proportional jitter (0.2) adds chaos to long lines
	// Scaling factor applies to the perpendicular vector (length = dist)
	jitterScale := 0.2 + (3.0 / dist)

	points := make([]struct{ X, Y int }, 0, segments+1)
	points = append(points, struct{ X, Y int }{x1, y1})

	for i := 1; i < segments; i++ {
		t := float64(i) / float64(segments)

		// Linear interpolation point
		bx := float64(x1) + float64(dx)*t
		by := float64(y1) + float64(dy)*t

		// Perpendicular jitter vector (-dy, dx) scaled
		jitter := jitterScale * (rng.Float64() - 0.5)

		jx := -float64(dy) * jitter
		jy := float64(dx) * jitter

		points = append(points, struct{ X, Y int }{
			int(bx + jx),
			int(by + jy),
		})
	}

	points = append(points, struct{ X, Y int }{x2, y2})
	return points
}

// drawLine uses Bresenham's algorithm with BlendScreen
func (r *LightningRenderer) drawLine(ctx render.RenderContext, buf *render.RenderBuffer, x0, y0, x1, y1 int, color render.RGB, alpha float64) {
	dx := x1 - x0
	if dx < 0 {
		dx = -dx
	}
	dy := y1 - y0
	if dy < 0 {
		dy = -dy
	}
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx - dy

	for {
		screenX := ctx.GameX + x0
		screenY := ctx.GameY + y0

		// Bounds check
		if screenX >= ctx.GameX && screenX < ctx.Width && screenY >= ctx.GameY && screenY < ctx.GameY+ctx.GameHeight {
			// BlendScreen adds light without blowing out background details immediately
			// Preserves text readability better than direct replacement
			buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, alpha, terminal.AttrNone)
		}

		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}