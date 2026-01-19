package renderer

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// HeatMeterRenderer draws the heat meter bar at the top of the screen
type HeatMeterRenderer struct {
	gameCtx *engine.GameContext

	burstBlink bool

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

// density256Chars provides intensity variants for trail, glow effects, ordered from lowest to highest density
var density256Chars = [4]rune{
	'\u2591', // ░ - light shade (25%) - was 176
	'\u2592', // ▒ - medium shade (50%) - was 177
	'\u2593', // ▓ - dark shade (75%) - was 178
	'\u2588', // █ - full block (100%) - was 219
}

// heatCellRenderer function type definition specifying signature for renderer callback
// Defines the interface for rendering strategy (256-color vs TrueColor) selected initialization
type heatCellRenderer func(buf *render.RenderBuffer, x, width int, fillRune rune)

// NewHeatMeterRenderer creates a heat meter renderer
func NewHeatMeterRenderer(ctx *engine.GameContext) *HeatMeterRenderer {
	r := &HeatMeterRenderer{
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
func (r *HeatMeterRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(constant.MaskUI)

	// Calculate Fill Limit from HeatComponent
	heatComp, ok := r.gameCtx.World.Components.Heat.GetComponent(r.gameCtx.World.Resources.Cursor.Entity)
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
		overheatRune = density256Chars[overheat/25]
	} else {
		overheatRune = 0
	}

	// Render Loop
	for x := 0; x <= maxX; x++ {
		// No early exit optimization, must clear the rest of the bar to Black/Empty
		if x > heatFillWidth || heatFillWidth == 0 {
			buf.SetBgOnly(x, 0, render.RgbBlack)
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
func (r *HeatMeterRenderer) cellTrueColor(buf *render.RenderBuffer, x, width int, fillRune rune) {
	lutIdx := (x * 255) / (width - 1)
	color := render.HeatGradientLUT[lutIdx]

	separatorPos := segmentIndex(x, width) != segmentIndex(x+1, width)
	if x > 0 && separatorPos {
		if !r.burstBlink {
			color = render.Scale(color, 0.5)
		} else {
			color = render.RgbRed
		}
	} else {
		fillRune = 0
	}

	if fillRune == 0 {
		buf.SetBgOnly(x, 0, color)
	} else {
		buf.SetWithBg(x, 0, fillRune, render.RgbWhite, color)
	}
}

// cell256 renders with fixed 10-segment palette colors
func (r *HeatMeterRenderer) cell256(buf *render.RenderBuffer, x, width int, fillRune rune) {
	if fillRune != 0 {
		buf.SetFgOnly(x, 0, fillRune, render.RgbWhite, terminal.AttrNone)
	} else {
		segment := segmentIndex(x, width)
		buf.SetBg256(x, 0, heat256LUT[segment])
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