package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// hudLine is one prepared panel row; a header carries no value
type hudLine struct {
	key    string
	value  string
	header bool
}

// PinnedStatsRenderer draws pinned metric groups over the game area, reading
// live registry values rather than the overlay's content snapshot
type PinnedStatsRenderer struct {
	gameCtx *engine.GameContext
	reg     *status.Registry

	bound *[]string // Pin snapshot the views were built from
	views []status.GroupView
	lines []hudLine
}

// NewPinnedStatsRenderer creates the pinned stats HUD renderer
func NewPinnedStatsRenderer(gameCtx *engine.GameContext) *PinnedStatsRenderer {
	return &PinnedStatsRenderer{
		gameCtx: gameCtx,
		reg:     gameCtx.World.Resources.Status,
	}
}

// IsVisible implements render.VisibilityToggle
func (r *PinnedStatsRenderer) IsVisible() bool {
	return r.gameCtx.OverlayHUD.Load() && !r.gameCtx.IsOverlayActive()
}

// Render draws the panel in the top-right of the viewport
func (r *PinnedStatsRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	r.rebind()
	if len(r.views) == 0 {
		return
	}

	maxH := ctx.ViewportHeight - 2*parameter.HudMarginY
	width := r.collect(maxH)
	if width < 1 || len(r.lines) == 0 {
		return
	}

	x := ctx.GameXOffset + ctx.ViewportWidth - width - parameter.HudMarginX
	y := ctx.GameYOffset + parameter.HudMarginY
	if x < ctx.GameXOffset {
		return // Viewport too narrow for a legible panel
	}

	buf.SetWriteMask(visual.MaskUI)

	// Tint the panel background and drop the layer tags underneath, so
	// occlusion dimming cannot blotch it where game glyphs used to be
	for row := range len(r.lines) {
		for col := range width {
			buf.Set(x+col, y+row, ' ', color.RGB{}, visual.RgbHudBg,
				render.BlendAlphaBg, parameter.HudBgAlpha, terminal.AttrNone)
			buf.ResetMask(x+col, y+row)
		}
	}

	// Blank the game glyphs under the panel and blend its background
	for row := range len(r.lines) {
		for col := range width {
			buf.Set(x+col, y+row, ' ', color.RGB{}, visual.RgbHudBg,
				render.BlendAlphaBg, parameter.HudBgAlpha, terminal.AttrNone)
		}
	}

	for i := range r.lines {
		r.drawLine(buf, x, y+i, width, &r.lines[i])
	}
}

// rebind rebuilds the group views when the pin set changes
func (r *PinnedStatsRenderer) rebind() {
	pins := r.gameCtx.OverlayPinsRef()
	if pins == r.bound {
		return
	}
	r.bound = pins
	r.views = r.views[:0]
	if pins == nil {
		return
	}
	for _, key := range *pins {
		if v, ok := r.reg.GroupView(key); ok {
			r.views = append(r.views, v)
		}
	}
}

// collect reads current values into the line buffer and returns the panel width
func (r *PinnedStatsRenderer) collect(maxH int) int {
	r.lines = r.lines[:0]
	width := 0

	for vi := range r.views {
		v := &r.views[vi]
		if len(r.lines)+1+v.Len() > maxH {
			break // A group is drawn whole or not at all
		}
		r.lines = append(r.lines, hudLine{key: v.Name(), header: true})
		width = max(width, tui.RuneLen(v.Name()))

		for i := range v.Len() {
			name, val := v.MetricName(i), v.Value(i)
			r.lines = append(r.lines, hudLine{key: name, value: val})
			width = max(width, tui.RuneLen(name)+parameter.HudColumnGap+tui.RuneLen(val))
		}
	}

	return min(max(width, parameter.HudMinWidth), parameter.HudMaxWidth)
}

// drawLine renders a header or a name/value pair, value right-aligned
func (r *PinnedStatsRenderer) drawLine(buf *render.RenderBuffer, x, y, width int, l *hudLine) {
	if l.header {
		name := tui.Truncate(l.key, width)
		drawFg(buf, x, y, name, visual.RgbHudHeader, terminal.AttrBold)
		return
	}

	val := l.value
	keyW := width - parameter.HudColumnGap - tui.RuneLen(val)
	if keyW < 1 {
		val = tui.Truncate(val, max(width-2, 1))
		keyW = width - parameter.HudColumnGap - tui.RuneLen(val)
	}
	drawFg(buf, x, y, tui.Truncate(l.key, max(keyW, 1)), visual.RgbHudKey, terminal.AttrNone)
	drawFg(buf, x+width-tui.RuneLen(val), y, val, visual.RgbHudValue, terminal.AttrNone)
}

// drawFg writes text over the blended panel background
func drawFg(buf *render.RenderBuffer, x, y int, s string, fg color.RGB, attr terminal.Attr) {
	for i, ch := range s {
		buf.SetFgOnly(x+i, y, ch, fg, attr)
	}
}
