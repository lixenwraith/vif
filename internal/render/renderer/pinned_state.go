package renderer

import (
	"strconv"
	"strings"

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

// hudGroup locates one whole pinned card in the prepared line buffer.
type hudGroup struct {
	name         string
	first, count int
	column, rowY int
}

// hudColumn records the used height of one wrapped HUD column.
type hudColumn struct {
	height int
}

// PinnedStatsRenderer draws pinned metric groups over the game area, reading
// live registry values rather than the overlay's content snapshot
type PinnedStatsRenderer struct {
	gameCtx *engine.GameContext
	reg     *status.Registry

	bound   *[]string // Pin snapshot the views were built from
	views   []status.GroupView
	lines   []hudLine
	groups  []hudGroup
	columns []hudColumn
	hidden  []string

	panelWidth   int
	shrinkWidth  int
	shrinkFrames int
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

// Render draws height-wrapped pinned cards from the viewport's top-left.
func (r *PinnedStatsRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	r.rebind()
	if len(r.views) == 0 {
		return
	}

	maxH := ctx.ViewportHeight - 2*parameter.HudMarginY
	maxW := ctx.ViewportWidth - 2*parameter.HudMarginX
	width := r.collect(maxW, maxH)
	if width < 1 || maxH < 1 || (len(r.columns) == 0 && len(r.hidden) == 0) {
		return
	}

	x, y := pinnedHUDOrigin(ctx)
	buf.SetWriteMask(visual.MaskUI)

	noticeRows := 0
	if len(r.hidden) != 0 {
		panelW := width
		if len(r.columns) > 1 {
			panelW = len(r.columns)*width + (len(r.columns)-1)*parameter.HudWrapGap
		}
		fillHUDRect(buf, x, y, panelW, 1)
		notice := "! hidden(" + strconv.Itoa(len(r.hidden)) + "): " + strings.Join(r.hidden, ",")
		notice = tui.Truncate(notice, panelW)
		drawFg(buf, x, y, notice, visual.RgbHudHeader, terminal.AttrBold)
		noticeRows = 1
	}

	// Each card remains whole within a column; only columns wrap horizontally.
	for ci := range r.columns {
		colX := x + ci*(width+parameter.HudWrapGap)
		fillHUDRect(buf, colX, y+noticeRows, width, r.columns[ci].height)
	}
	for gi := range r.groups {
		group := &r.groups[gi]
		if group.column < 0 {
			continue
		}
		lineX := x + group.column*(width+parameter.HudWrapGap)
		lineY := y + noticeRows + group.rowY
		for i := range group.count {
			r.drawLine(buf, lineX, lineY+i, width, &r.lines[group.first+i])
		}
	}
}

// pinnedHUDOrigin anchors width changes at the viewport's top-left.
func pinnedHUDOrigin(ctx render.RenderContext) (x, y int) {
	return ctx.GameXOffset + parameter.HudMarginX, ctx.GameYOffset + parameter.HudMarginY
}

// fillHUDRect blanks game glyphs, removes their layer tags, and blends the panel.
func fillHUDRect(buf *render.RenderBuffer, x, y, width, height int) {
	for row := range height {
		for col := range width {
			buf.Set(x+col, y+row, ' ', color.RGB{}, visual.RgbHudBg,
				render.BlendAlphaBg, parameter.HudBgAlpha, terminal.AttrNone)
			buf.ResetMask(x+col, y+row)
			buf.Set(x+col, y+row, ' ', color.RGB{}, visual.RgbHudBg,
				render.BlendAlphaBg, parameter.HudBgAlpha, terminal.AttrNone)
		}
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
	r.panelWidth, r.shrinkWidth, r.shrinkFrames = 0, 0, 0
	if pins == nil {
		return
	}
	for _, key := range *pins {
		if v, ok := r.reg.GroupView(key); ok {
			r.views = append(r.views, v)
		}
	}
}

// collect reads current values, stabilizes width, and packs whole groups into columns.
func (r *PinnedStatsRenderer) collect(maxW, maxH int) int {
	r.lines = r.lines[:0]
	r.groups = r.groups[:0]
	r.columns = r.columns[:0]
	r.hidden = r.hidden[:0]
	if maxW < 1 || maxH < 1 {
		return 0
	}
	desiredWidth := 0

	for vi := range r.views {
		v := &r.views[vi]
		if !v.Visible() {
			continue
		}
		first := len(r.lines)
		r.lines = append(r.lines, hudLine{key: v.Name(), header: true})
		desiredWidth = max(desiredWidth, tui.RuneLen(v.Name()))

		for i := range v.Len() {
			name, val := v.MetricName(i), v.Value(i)
			r.lines = append(r.lines, hudLine{key: name, value: val})
			desiredWidth = max(desiredWidth, tui.RuneLen(name)+parameter.HudColumnGap+tui.RuneLen(val))
		}
		r.groups = append(r.groups, hudGroup{
			name: v.Name(), first: first, count: 1 + v.Len(), column: -1,
		})
	}
	if len(r.groups) == 0 {
		return 0
	}

	desiredWidth = min(max(desiredWidth, parameter.HudMinWidth), parameter.HudMaxWidth)
	width := min(r.stabilizeWidth(desiredWidth), maxW)
	maxColumns := max((maxW+parameter.HudWrapGap)/(width+parameter.HudWrapGap), 1)
	r.pack(maxH, maxColumns)
	if len(r.hidden) != 0 && maxH > 1 {
		r.pack(maxH-1, maxColumns)
	}
	return width
}

// stabilizeWidth expands immediately and contracts after a full narrow window.
func (r *PinnedStatsRenderer) stabilizeWidth(desired int) int {
	if desired >= r.panelWidth {
		r.panelWidth = desired
		r.shrinkWidth, r.shrinkFrames = 0, 0
		return r.panelWidth
	}

	if r.shrinkFrames == 0 {
		r.shrinkWidth = desired
	} else {
		r.shrinkWidth = max(r.shrinkWidth, desired)
	}
	r.shrinkFrames++
	if r.shrinkFrames >= parameter.HudWidthShrinkFrames {
		r.panelWidth = r.shrinkWidth
		r.shrinkWidth, r.shrinkFrames = 0, 0
	}
	return r.panelWidth
}

// pack fills columns top-to-bottom and records every group that cannot fit.
func (r *PinnedStatsRenderer) pack(maxH, maxColumns int) {
	r.columns = r.columns[:0]
	r.hidden = r.hidden[:0]
	for i := range r.groups {
		r.groups[i].column, r.groups[i].rowY = -1, 0
	}

	for gi := range r.groups {
		group := &r.groups[gi]
		if group.count > maxH {
			r.hidden = append(r.hidden, group.name)
			continue
		}

		column := -1
		for ci := range r.columns {
			if r.columns[ci].height+group.count <= maxH {
				column = ci
				break
			}
		}
		if column < 0 {
			if len(r.columns) >= maxColumns {
				r.hidden = append(r.hidden, group.name)
				continue
			}
			r.columns = append(r.columns, hudColumn{})
			column = len(r.columns) - 1
		}

		group.column = column
		group.rowY = r.columns[column].height
		r.columns[column].height += group.count
	}
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
