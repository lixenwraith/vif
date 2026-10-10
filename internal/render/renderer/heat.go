package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
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

// Render implements SystemRenderer. The unlit track is black, and with no local
// cursor the whole bar is that empty track rather than the theme background.
func (r *HeatRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)

	var heat, overheat int
	r.burstBlink = false
	if player := r.gameCtx.World.Resources.Player; player.Valid() {
		if heatComp, ok := r.gameCtx.World.Components.Heat.GetPtr(player.Entity); ok {
			heat, overheat = heatComp.Current, heatComp.Overheat
		}
		if view, ok := r.gameCtx.World.Components.CursorView.GetPtr(player.Entity); ok {
			r.burstBlink = view.BurstFlashRemaining > 0
		}
	}

	width := ctx.ScreenWidth
	lit, overheatLit := heatBarLit(heat, width), heatBarLit(overheat, width)
	overheatRune := r.density[min(overheat/25, len(r.density)-1)]

	for x := range width {
		switch {
		case x >= lit:
			buf.SetBgOnly(x, 0, visual.RgbBlack)
		case x < overheatLit:
			r.renderCell(buf, x, width, overheatRune)
		default:
			r.renderCell(buf, x, width, 0)
		}
	}
}

// cellTrueColor renders with smooth gradient, only a notch carrying the overheat glyph
func (r *HeatRenderer) cellTrueColor(buf *render.RenderBuffer, x, width int, fillRune rune) {
	c := render.HeatGradientLUT[(x*255)/(width-1)]

	if heatNotch(x, width) {
		if r.burstBlink {
			c = visual.RgbRed
		} else {
			c = color.Scale(c, 0.5)
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

// heatBarLit returns how many bar cells, from the left, a 0-100 level lights
func heatBarLit(level, width int) int {
	return width * min(max(level, 0), 100) / 100
}

// segmentIndex returns which tenth of the bar column x lies in. It inverts
// heatBarLit, so a level of 10k lights exactly the first k tenths at any width,
// and the notch closing each tenth is the last cell lit.
func segmentIndex(x, width int) int {
	return min(max((10*(x+1)-1)/max(width, 1), 0), 9)
}

// heatNotch reports whether column x is the last cell of its tenth
func heatNotch(x, width int) bool {
	return segmentIndex(x, width) != segmentIndex(x+1, width)
}

// heatLead256 returns the palette colour of the bar's last lit cell
func heatLead256(heat, width int) uint8 {
	return visual.Heat256LUT[segmentIndex(heatBarLit(heat, width)-1, width)]
}
