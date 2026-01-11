package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// SpiritRenderer draws converging spirit entities with blinking effect
type SpiritRenderer struct {
	gameCtx *engine.GameContext
}

func NewSpiritRenderer(gameCtx *engine.GameContext) *SpiritRenderer {
	return &SpiritRenderer{
		gameCtx: gameCtx,
	}
}

func (r *SpiritRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Spirit.AllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	// Calculate blink phase from frame number
	// SpiritBlinkHz cycles per second, ~60 frames/sec
	blinkPhase := (ctx.FrameNumber * int64(constant.SpiritBlinkHz) / 60) % 2

	// Configuration for the trail effect
	const (
		trailSteps = 10               // Number of segments (Head + 9 trail segments)
		trailLag   = vmath.Scale / 60 // Fixed progress lag per segment (~1.6%)
	)

	for _, entity := range entities {
		spirit, ok := r.gameCtx.World.Components.Spirit.GetComponent(entity)
		if !ok {
			continue
		}

		// Pre-calculate invariant vector and aspect-corrected Y for spiral math
		relX := spirit.StartX - spirit.TargetX
		relY := spirit.StartY - spirit.TargetY
		relYCirc := vmath.ScaleToCircular(relY)

		// Render loop: Draw head (i=0) and trailing segments (i>0)
		for i := 0; i < trailSteps; i++ {
			// Calculate progress for this segment
			p := spirit.Progress - int64(i)*trailLag
			if p < 0 {
				continue // Segment hasn't spawned yet
			}

			// --- Spiral Trajectory Calculation ---

			// 1. Calculate rotation at current progress
			currentRot := vmath.Mul(p, spirit.Spin)

			// 2. Rotate the aspect-corrected relative vector
			rotX, rotYCirc := vmath.RotateVector(relX, relYCirc, currentRot)

			// 3. Apply convergence scale (1.0 at start -> 0.0 at target)
			scale := vmath.Scale - p
			rotX = vmath.Mul(rotX, scale)
			rotYCirc = vmath.Mul(rotYCirc, scale)

			// 4. Restore aspect ratio (Circular -> Elliptical)
			rotY := vmath.ScaleFromCircular(rotYCirc)

			// 5. Map to screen space
			screenX := ctx.GameXOffset + vmath.ToInt(spirit.TargetX+rotX)
			screenY := ctx.GameYOffset + vmath.ToInt(spirit.TargetY+rotY)

			// Bounds check
			if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
				screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
				continue
			}

			// --- Coloring & Fading ---

			var color render.RGB
			var alpha float64 = 1.0

			if i == 0 {
				// Head: Use blink logic and distance-based intensity
				if blinkPhase == 0 {
					color = spirit.BaseColor
				} else {
					color = spirit.BlinkColor
				}
				// Intensity increases as it approaches target (0.5 -> 1.0)
				intensity := 0.5 + (vmath.ToFloat(p) * 0.5)
				color = render.Scale(color, intensity)
			} else {
				// Trail: Use BaseColor with fast quadratic fade
				color = spirit.BaseColor

				// Normalized position in trail (0.0 to 1.0)
				trailPos := float64(i) / float64(trailSteps)

				// Quadratic falloff: (1 - x)^2
				fade := 1.0 - trailPos
				fade = fade * fade

				color = render.Scale(color, fade)
				// Reduce alpha blend weight for tail to make it ghostly
				alpha = fade
			}

			// Additive blend for glow effect
			buf.Set(screenX, screenY, spirit.Rune, color, render.RGBBlack, render.BlendAddFg, alpha, terminal.AttrNone)
		}
	}
}