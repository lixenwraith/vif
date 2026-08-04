package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
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
	spirits := r.gameCtx.World.Components.Spirit
	if spirits.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	// TODO: move to parameter
	// Configuration for the trail effect
	const (
		trailSteps = 10               // Number of segments (Head + 9 trail segments)
		trailLag   = vmath.Scale / 60 // Fixed progress lag per segment (~1.6%)
	)

	spirits.Each(func(_ core.Entity, spiritComp *component.SpiritComponent) bool {
		// Pre-calculate invariant vector and aspect-corrected Y for spiral math
		relX := spiritComp.StartX - spiritComp.TargetX
		relY := spiritComp.StartY - spiritComp.TargetY
		relYCirc := vmath.ScaleToCircular(relY)

		// Render loop: Draw head (i=0) and trailing segments (i>0)
		for i := range trailSteps {
			// Calculate progress for this segment
			p := spiritComp.Progress - int64(i)*trailLag
			if p < 0 {
				continue // Segment hasn't spawned yet
			}

			// --- Spiral Trajectory Calculation ---

			// 1. Calculate rotation at current progress
			currentRot := vmath.Mul(p, spiritComp.Spin)

			// 2. Rotate the aspect-corrected relative vector
			rotX, rotYCirc := vmath.RotateVector(relX, relYCirc, currentRot)

			// 3. Apply convergence scale (1.0 at start -> 0.0 at target)
			scale := vmath.Scale - p
			rotX = vmath.Mul(rotX, scale)
			rotYCirc = vmath.Mul(rotYCirc, scale)

			// 4. Restore aspect ratio (Circular -> Elliptical)
			rotY := vmath.ScaleFromCircular(rotYCirc)

			// 5. Map to screen space

			// Compute map position
			mapX := vmath.ToInt(spiritComp.TargetX + rotX)
			mapY := vmath.ToInt(spiritComp.TargetY + rotY)

			// Transform to screen coords
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			// --- Coloring & Fading ---

			var c color.RGB
			var alpha = 1.0

			if i == 0 {
				// Head: color cycle through gradient based on progress
				c = spiritProgressColor(spiritComp.BaseColor, p)
				// Intensity increases as it approaches target (0.5 -> 1.0)
				intensity := 0.5 + (vmath.ToFloat(p) * 0.5)
				c = color.Scale(c, intensity)
			} else {
				// Trail: inherit cycled color with linear fade + boosted alpha
				trailProgress := p - int64(i-1)*trailLag
				if trailProgress < 0 {
					trailProgress = 0
				}
				c = spiritProgressColor(spiritComp.BaseColor, trailProgress)

				// Normalized position in trail (0.0 to 1.0)
				trailPos := float64(i) / float64(trailSteps)

				fade := 1.0 - trailPos
				fade = fade * 1.3
				if fade > 1.0 {
					fade = 1.0
				}

				c = color.Scale(c, fade)
				alpha = 0.4 + fade*0.6
			}

			// Additive blend for glow effect
			buf.Set(screenX, screenY, spiritComp.Rune, c, visual.RgbBlack, render.BlendAddFg, alpha, terminal.AttrNone)
		}
		return true
	})
}

// spiritProgressColor returns gradient color based on base and animation progress
func spiritProgressColor(base component.SpiritColor, progress int64) color.RGB {
	offset := visual.SpiritBaseOffsets[base]
	// Progress adds 0-128 to cycle through ~half the gradient
	progressOffset := int((progress * 128) >> vmath.Shift)
	lutIdx := (offset + progressOffset) & 0xFF

	return render.HeatGradientLUT[lutIdx]
}
