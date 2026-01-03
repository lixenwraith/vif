package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// HeatMeterRenderer draws the heat meter bar at the top of the screen
type HeatMeterRenderer struct {
	gameCtx   *engine.GameContext
	heatStore *engine.Store[component.HeatComponent]

	renderCell heatCellRenderer
}

// heat256LUT contains xterm 256-palette indices for 10 heat segments
// Progression: deep red → orange → yellow → green → cyan → blue → purple
// Indices from 6×6×6 color cube: index = 16 + 36*r + 6*g + b where r,g,b ∈ [0,5]
var heat256LUT = [10]uint8{
	196, // 0-10%:   Red (5,0,0)
	202, // 10-20%:  Red-orange (5,1,0)
	208, // 20-30%:  Orange (5,2,0)
	220, // 30-40%:  Yellow-orange (5,4,0)
	154, // 40-50%:  Yellow-green (3,5,0)
	46,  // 50-60%:  Green (0,5,0)
	51,  // 60-70%:  Cyan (0,5,5)
	33,  // 70-80%:  Blue (0,2,5)
	63,  // 80-90%:  Blue-purple (1,1,5)
	129, // 90-100%: Purple (3,0,5)
}

// heatCellRenderer function type definition specifying signature for renderer callback
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type heatCellRenderer func(buf *render.RenderBuffer, x, width int)

// NewHeatMeterRenderer creates a heat meter renderer
func NewHeatMeterRenderer(ctx *engine.GameContext) *HeatMeterRenderer {
	h := &HeatMeterRenderer{
		gameCtx:   ctx,
		heatStore: engine.GetStore[component.HeatComponent](ctx.World),
	}

	// Access RenderConfig for display mode
	cfg := engine.MustGetResource[*engine.RenderConfig](ctx.World.ResourceStore)

	if cfg.ColorMode == terminal.ColorMode256 {
		h.renderCell = h.cell256
	} else {
		h.renderCell = h.cellTrueColor
	}
	return h
}

// Render implements SystemRenderer
func (r *HeatMeterRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskUI)

	// Calculate Fill Limit from HeatComponent
	heat := 0
	if hc, ok := r.heatStore.Get(r.gameCtx.CursorEntity); ok {
		heat = int(hc.Current.Load())
	}
	fillWidth := (ctx.Width * heat) / 100

	// Render Loop
	for x := 0; x < ctx.Width; x++ {
		// No early exit optimization, must clear the rest of the bar to Black/Empty
		if x >= fillWidth {
			// Draw Empty
			buf.SetBgOnly(x, 0, render.RgbBlack)
			continue
		}
		r.renderCell(buf, x, ctx.Width)
	}
}

// cellTrueColor renders with smooth gradient
func (r *HeatMeterRenderer) cellTrueColor(buf *render.RenderBuffer, x, width int) {
	lutIdx := (x * 255) / (width - 1)
	color := render.HeatGradientLUT[lutIdx]

	// Separator dimming at 10% intervals
	// Use float to minimize drift over width
	sepInterval := float64(width) / 10.0
	sepPos := int(float64(x) / sepInterval)
	if x > 0 && float64(x) >= float64(sepPos)*sepInterval && float64(x) < float64(sepPos)*sepInterval+1 {
		color = render.Scale(color, 0.5)
	}

	buf.SetBgOnly(x, 0, color)
}

// cell256 renders with fixed 10-segment palette colors
func (r *HeatMeterRenderer) cell256(buf *render.RenderBuffer, x, width int) {
	// Map x position to segment index (0-9)
	segment := (x * 10) / width
	if segment > 9 {
		segment = 9
	}
	buf.SetBg256(x, 0, heat256LUT[segment])
}