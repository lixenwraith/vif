package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// HeatMeterRenderer draws the heat meter bar at the top of the screen
type HeatMeterRenderer struct {
	state *engine.GameState
}

// NewHeatMeterRenderer creates a heat meter renderer
func NewHeatMeterRenderer(state *engine.GameState) *HeatMeterRenderer {
	return &HeatMeterRenderer{state: state}
}

// Render implements SystemRenderer
func (h *HeatMeterRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	heat := h.state.GetHeat()

	// Calculate display segments: 0-9=0, 10-19=1, ..., 90-99=9, 100=10
	displayHeat := heat / 10
	if displayHeat > 10 {
		displayHeat = 10
	}

	// Draw 10-segment heat bar across full terminal width
	segmentWidth := float64(ctx.Width) / 10.0
	for segment := 0; segment < 10; segment++ {
		// Calculate start and end positions for this segment
		segmentStart := int(float64(segment) * segmentWidth)
		segmentEnd := int(float64(segment+1) * segmentWidth)

		// Determine if this segment is filled
		isFilled := segment < displayHeat

		// Draw all characters in this segment
		for x := segmentStart; x < segmentEnd && x < ctx.Width; x++ {
			if isFilled {
				// Calculate progress for color gradient (0.0 to 1.0)
				progress := float64(segment+1) / 10.0
				color := render.GetHeatMeterColor(progress)

				// Optimization: Use SetBgOnly for filled segments
				// Since buffer is cleared to empty space, setting BG creates a solid block
				// This is faster than writing Rune+Fg+Bg
				buf.SetBgOnly(x, 0, color)
			} else {
				// Empty segment: ensure it is black/empty
				// Use SetBgOnly with Black to ensure "empty" look over default BG
				buf.SetBgOnly(x, 0, render.RgbBlack)
			}
		}
	}
}