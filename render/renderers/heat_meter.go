// @focus: #render { ui } #game { heat }
package renderers

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// HeatMeterRenderer draws the heat meter bar at the top of the screen
type HeatMeterRenderer struct {
	gameCtx    *engine.GameContext
	renderCell heatCellRenderer
}

// heatCellRenderer function type definition specifying signature for renderer callback
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type heatCellRenderer func(buf *render.RenderBuffer, x int, color render.RGB)

// NewHeatMeterRenderer creates a heat meter renderer
func NewHeatMeterRenderer(ctx *engine.GameContext) *HeatMeterRenderer {
	h := &HeatMeterRenderer{gameCtx: ctx}

	// Access RenderConfig for display mode
	cfg := engine.MustGetResource[*engine.RenderConfig](ctx.World.Resources)

	if cfg.ColorMode == uint8(terminal.ColorMode256) {
		h.renderCell = h.cell256
	} else {
		h.renderCell = h.cellTrueColor
	}
	return h
}

// Render implements SystemRenderer
func (h *HeatMeterRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)

	// 1. Calculate Fill Limit from HeatComponent
	heat := 0
	if hc, ok := world.Heats.Get(h.gameCtx.CursorEntity); ok {
		heat = int(hc.Current.Load())
	}
	// fillWidth = (Width * Heat) / 100
	fillWidth := (ctx.Width * heat) / 100

	// 2. Pre-calculate separator interval
	// Use float to minimize drift over width
	sepInterval := float64(ctx.Width) / 10.0
	nextSep := sepInterval

	// 3. Render Loop
	for x := 0; x < ctx.Width; x++ {
		// No early exit optimization, must clear the rest of the bar to Black/Empty
		if x >= fillWidth {
			// Draw Empty
			buf.SetBgOnly(x, 0, render.RgbBlack)
			continue
		}

		// Calculate Gradient Color
		// Map x (0..Width) to LUT (0..255)
		lutIdx := (x * 255) / (ctx.Width - 1)
		color := render.HeatGradientLUT[lutIdx]

		// Apply Separator (Subtle Dimming)
		// Check if x crosses a separator threshold, skipping first pixel for deep red start
		if x > 0 && float64(x) >= nextSep {
			// Dim the color by 50%
			color = render.Scale(color, 0.5)
			nextSep += sepInterval
		}

		// Draw Filled
		h.renderCell(buf, x, color)
	}
}

// cellTrueColor: Direct RGB write
func (h *HeatMeterRenderer) cellTrueColor(buf *render.RenderBuffer, x int, color render.RGB) {
	buf.SetBgOnly(x, 0, color)
}

// cell256: Direct write, relies on buffer's internal quantization if needed
// or if SetBgOnly expects RGB, this is fine as Output buffer handles mapping
func (h *HeatMeterRenderer) cell256(buf *render.RenderBuffer, x int, color render.RGB) {
	buf.SetBgOnly(x, 0, color)
}