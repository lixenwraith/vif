package renderers

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/core"
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

		var color core.RGB
		if isFilled {
			// Calculate progress for color gradient (0.0 to 1.0)
			progress := float64(segment+1) / 10.0
			color = render.GetHeatMeterColor(progress)
		} else {
			// Empty segment: black foreground
			color = render.RgbBlack
		}

		// Draw all characters in this segment
		for x := segmentStart; x < segmentEnd && x < ctx.Width; x++ {
			buf.SetPixel(x, 0, '█', color, render.DefaultBgRGB, render.BlendReplace, 1.0, tcell.AttrNone)
		}
	}
}