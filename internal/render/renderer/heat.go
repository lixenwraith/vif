package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

// HeatRenderer draws the heat meter bar at the top of the screen
type HeatRenderer struct {
	gameCtx *engine.GameContext

	burstBlink bool

	renderCell heatCellRenderer
	density    *[4]rune
}

// heatCellRenderer draws one filled bar cell in the colour mode chosen at construction
type heatCellRenderer func(buf *render.RenderBuffer, x, width int, fillRune rune)

// NewHeatRenderer creates a heat meter renderer
func NewHeatRenderer(ctx *engine.GameContext) *HeatRenderer {
	r := &HeatRenderer{
		gameCtx: ctx,
	}

	if r.gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell, r.density = r.cell256, &visual.Density256Chars
	} else {
		r.renderCell, r.density = r.cellTrueColor, &visual.DensityChars
	}
	return r
}

// Render implements SystemRenderer
func (r *HeatRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	if !r.gameCtx.World.Resources.Player.Valid() {
		return
	}

	// Calculate Fill Limit from HeatComponent
	heatComp, ok := r.gameCtx.World.Components.Heat.GetPtr(r.gameCtx.World.Resources.Player.Entity)
	if !ok {
		return
	}

	buf.SetWriteMask(visual.MaskUI)

	heat := heatComp.Current
	overheat := heatComp.Overheat
	r.burstBlink = false
	if view, ok := r.gameCtx.World.Components.CursorView.GetPtr(r.gameCtx.World.Resources.Player.Entity); ok {
		r.burstBlink = view.BurstFlashRemaining > 0
	}

	maxX := ctx.ScreenWidth - 1
	heatFillWidth := heatBarFill(heat, ctx.ScreenWidth)
	overheatFillWidth := heatBarFill(overheat, ctx.ScreenWidth)

	var overheatRune rune
	if overheat > 0 {
		overheatRune = r.density[overheat/25]
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
	c := render.HeatGradientLUT[lutIdx]

	separatorPos := segmentIndex(x, width) != segmentIndex(x+1, width)
	if x > 0 && separatorPos {
		if !r.burstBlink {
			c = color.Scale(c, 0.5)
		} else {
			c = visual.RgbRed
		}
	} else {
		fillRune = 0
	}

	if fillRune == 0 {
		buf.SetBgOnly(x, 0, c)
	} else {
		buf.SetWithBg(x, 0, fillRune, visual.RgbWhite, c)
	}
}

// cell256 renders with fixed 10-segment palette colors, overheat glyphs over the segment
func (r *HeatRenderer) cell256(buf *render.RenderBuffer, x, width int, fillRune rune) {
	buf.SetBg256(x, 0, visual.Heat256LUT[segmentIndex(x, width)])
	if fillRune != 0 {
		buf.SetFgOnly(x, 0, fillRune, visual.RgbWhite, terminal.AttrNone)
	}
}

// segmentIndex returns which of the ten 256-colour bar segments column x falls in
func segmentIndex(x, width int) int {
	return min(max(x*10/max(width-1, 1), 0), 9)
}

// heatBarFill returns the last bar column a 0-100 level fills
func heatBarFill(level, width int) int {
	return ((width - 1) * level) / 100
}

// heatLead256 returns the palette colour of the bar's last filled cell
func heatLead256(heat, width int) uint8 {
	return visual.Heat256LUT[segmentIndex(heatBarFill(min(max(heat, 0), 100), width), width)]
}
