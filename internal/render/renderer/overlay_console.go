package renderer

import (
	"fmt"
	"strings"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// consoleRow is one screen row of the console: a message's first row carries its
// stamp, the rest of it wraps under its text, and its last row may end with the
// count of its repeats, tail runes long
type consoleRow struct {
	stamp string
	text  string
	kind  parameter.StatusKind
	tail  int
}

// consoleView caches the console's rows, rebuilt when a message comes in or the
// width changes
type consoleView struct {
	lines []engine.ConsoleLine
	rows  []consoleRow
	seq   uint64
	width int
	built bool
}

// consoleColors are the console's text colours on this frame's tier
type consoleColors struct {
	kind  [parameter.StatusKindCount]color.RGB
	stamp color.RGB
}

// renderConsole draws the console in its band
func (r *OverlayRenderer) renderConsole(buf *render.RenderBuffer, g engine.OverlayGeometry) {
	if r.adapter == nil {
		r.adapter = NewTUIAdapter(g.W, g.H)
	} else if r.adapter.Width() != g.W || r.adapter.Height() != g.H {
		r.adapter.Resize(g.W, g.H)
	}
	r.adapter.Clear(visual.RgbOverlayBg)
	root := r.adapter.Region()

	rows := r.consoleRows(g.ContentW)
	visible, total := g.ContentH, len(rows)
	r.gameCtx.SetOverlayContentH(total)
	offset := min(r.gameCtx.GetOverlayScroll(), max(total-visible, 0))

	pal := r.consoleColors()
	body := root.Sub(g.ContentX, g.ContentY, g.ContentW, g.ContentH)
	textX := tui.RuneLen(parameter.ConsoleStamp) + 1
	top := max(visible-total, 0) // Fewer rows than the band sit at its foot, by the line
	for i := range min(visible, total) {
		row := rows[offset+i]
		y := top + i
		body.Text(0, y, row.stamp, pal.stamp, visual.RgbOverlayBg, terminal.AttrNone)
		text := []rune(row.text)
		split := len(text) - row.tail
		body.Text(textX, y, string(text[:split]), pal.kind[row.kind], visual.RgbOverlayBg, terminal.AttrNone)
		body.Text(textX+split, y, string(text[split:]), pal.stamp, visual.RgbOverlayBg, terminal.AttrNone)
	}

	if g.ScrollW > 0 {
		root.Sub(g.ScrollX, g.ContentY, g.ScrollW, g.ContentH).ScrollBarStyled(0, offset, visible, total, tui.ScrollBarOpts{
			ThumbFg: visual.RgbOverlayScrollThumb, TrackFg: visual.RgbOverlayScrollTrack,
			Bg: visual.RgbOverlayBg, HideIdle: true,
		})
	}

	hints := parameter.OverlayHintsConsole
	if r.gameCtx.IsCommandMode() {
		hints = parameter.OverlayHintsConsoleCommand
	}
	r.renderConsoleEdge(root, g, hints)

	r.adapter.FlushTo(buf, g.X, g.Y, visual.MaskUI)
	if r.exact {
		buf.KeepExact(render.RectWH(g.X, g.Y, g.W, g.H))
	}
}

// renderConsoleEdge draws the band's edge toward the map: a rule, the title at its
// start and the widest hint that fits at its end
func (r *OverlayRenderer) renderConsoleEdge(root tui.Region, g engine.OverlayGeometry, hints []string) {
	root.HLine(g.HintY, tui.LineSingle, visual.RgbConsoleEdge)
	title := " " + parameter.ConsoleTitle + " "
	root.Text(parameter.ConsoleEdgeInset, g.HintY, title, visual.RgbConsoleTitle, visual.RgbOverlayBg, terminal.AttrBold)
	free := g.W - 2*parameter.ConsoleEdgeInset - tui.RuneLen(title) - parameter.ConsoleEdgeGap
	for _, hint := range hints {
		if hint = " " + hint + " "; tui.RuneLen(hint) <= free {
			root.Text(g.W-parameter.ConsoleEdgeInset-tui.RuneLen(hint), g.HintY, hint, visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrNone)
			return
		}
	}
}

// consoleRows lays the console's messages out at width cells: the stamp, then the
// text wrapped with a hanging indent under its first word
func (r *OverlayRenderer) consoleRows(width int) []consoleRow {
	v := &r.consoleView
	if seq := r.gameCtx.ConsoleSeq(); v.built && seq == v.seq && width == v.width {
		return v.rows
	}
	v.lines, v.seq = r.gameCtx.ConsoleLines(v.lines[:0])
	v.width, v.built = width, true
	v.rows = v.rows[:0]

	stampW := tui.RuneLen(parameter.ConsoleStamp) + 1
	textW := max(width-stampW, 1)
	for _, line := range v.lines {
		text, tail := line.Text, ""
		if line.Count > 1 {
			tail = fmt.Sprintf(parameter.ConsoleRepeat, line.Count)
		}
		wrapped := tui.WrapText(sanitizeConsole(text)+tail, textW)
		stamp := time.Unix(0, line.At).Format(parameter.ConsoleStamp) // local time
		for i, part := range wrapped {
			row := consoleRow{text: part, kind: line.Kind}
			if i == 0 {
				row.stamp = stamp
			}
			if i == len(wrapped)-1 && tail != "" {
				row.tail = min(tui.RuneLen(strings.TrimLeft(tail, " ")), tui.RuneLen(part))
			}
			v.rows = append(v.rows, row)
		}
	}
	return v.rows
}

// sanitizeConsole keeps a message on its rows: a control rune would move the
// terminal's cursor, so each shows as a space
func sanitizeConsole(s string) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}

// consoleColors picks the console's colours for the tier drawn on
func (r *OverlayRenderer) consoleColors() consoleColors {
	if r.console == nil {
		return consoleColors{kind: visual.RgbConsoleKind, stamp: visual.RgbConsoleStamp}
	}
	var p consoleColors
	for k, entry := range visual.ConsoleKind256 {
		p.kind[k] = r.console.Color(entry)
	}
	p.stamp = r.console.Color(visual.ConsoleStamp256)
	return p
}
