package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// HeatRenderer draws the heat meter bar at the top of the screen
type HeatRenderer struct {
	gameCtx *engine.GameContext

	burstBlink bool

	renderCell heatCellRenderer
}

// heatCellRenderer function type definition specifying signature for renderer callback
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type heatCellRenderer func(buf *render.RenderBuffer, x, width int, fillRune rune)

// NewHeatRenderer creates a heat meter renderer
func NewHeatRenderer(ctx *engine.GameContext) *HeatRenderer {
	r := &HeatRenderer{
		gameCtx: ctx,
	}

	if r.gameCtx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

// Render implements SystemRenderer
func (r *HeatRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	// Calculate Fill Limit from HeatComponent
	heatComp, ok := r.gameCtx.World.Components.Heat.GetComponent(r.gameCtx.World.Resources.Player.Entity)
	if !ok {
		return
	}
	heat := heatComp.Current
	overheat := heatComp.Overheat
	r.burstBlink = heatComp.BurstFlashRemaining > 0

	maxX := ctx.ScreenWidth - 1
	heatFillWidth := (maxX * heat) / 100
	overheatFillWidth := (maxX * overheat) / 100

	var overheatRune rune
	if overheat > 0 {
		overheatRune = visual.Density256Chars[overheat/25]
	} else {
		overheatRune = 0
	}

	// Render Loop
	for x := 0; x <= maxX; x++ {
		// No early exit optimization, must clear the rest of the bar to Black/Empty
		if x > heatFillWidth || heatFillWidth == 0 {
			buf.SetBgOnly(x, 0, visual.RgbBlack)
			continue
		}

		if x > overheatFillWidth || overheatFillWidth == 0 {
			r.renderCell(buf, x, ctx.ScreenWidth, 0)
		} else {
			r.renderCell(buf, x, ctx.ScreenWidth, overheatRune)
		}
	}
}

// cellTrueColor renders with smooth gradient
func (r *HeatRenderer) cellTrueColor(buf *render.RenderBuffer, x, width int, fillRune rune) {
	lutIdx := (x * 255) / (width - 1)
	color := render.HeatGradientLUT[lutIdx]

	separatorPos := segmentIndex(x, width) != segmentIndex(x+1, width)
	if x > 0 && separatorPos {
		if !r.burstBlink {
			color = render.Scale(color, 0.5)
		} else {
			color = visual.RgbRed
		}
	} else {
		fillRune = 0
	}

	if fillRune == 0 {
		buf.SetBgOnly(x, 0, color)
	} else {
		buf.SetWithBg(x, 0, fillRune, visual.RgbWhite, color)
	}
}

// cell256 renders with fixed 10-segment palette colors
func (r *HeatRenderer) cell256(buf *render.RenderBuffer, x, width int, fillRune rune) {
	if fillRune != 0 {
		buf.SetFgOnly(x, 0, fillRune, visual.RgbWhite, terminal.AttrNone)
	} else {
		segment := segmentIndex(x, width)
		buf.SetBg256(x, 0, visual.Heat256LUT[segment])
	}
}

// segmentIndex calculates which segment in which segment of the heat bar the X coordinate falls into
func segmentIndex(x, width int) int {
	segment := (x * 10) / (width - 1)
	if segment > 9 {
		segment = 9
	}
	return segment
}