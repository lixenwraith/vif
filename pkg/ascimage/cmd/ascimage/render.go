package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
)

// palette is the chrome's colors; on 16 colors and none the cursor row is
// reversed too, as an RGB of its own may land on the panel's entry
type palette struct {
	bg, fg, dim, accent, accent2, bar, barFg, sel, border, ok, warn color.RGB
	reverse                                                         bool
}

func paletteFor(mode terminal.ColorMode) palette {
	return palette{
		bg: color.RGB{R: 26, G: 27, B: 38}, fg: color.RGB{R: 192, G: 202, B: 245},
		dim: color.RGB{R: 86, G: 95, B: 137}, accent: color.RGB{R: 122, G: 162, B: 247},
		accent2: color.RGB{R: 187, G: 154, B: 247}, bar: color.RGB{R: 22, G: 22, B: 30},
		barFg: color.RGB{R: 140, G: 152, B: 200}, sel: color.RGB{R: 45, G: 50, B: 80},
		border: color.RGB{R: 59, G: 66, B: 97}, ok: color.RGB{R: 158, G: 206, B: 106},
		warn:    color.RGB{R: 224, G: 175, B: 104},
		reverse: mode == terminal.ColorMode16 || mode == terminal.ColorModeNone,
	}
}

// backdrops are what shows under an image's transparent cells and around it
var backdrops = []struct {
	name string
	a, b color.RGB // a checker's two squares; a plain backdrop is one
}{
	{"panel", color.RGB{R: 26, G: 27, B: 38}, color.RGB{R: 26, G: 27, B: 38}},
	{"black", color.RGB{}, color.RGB{}},
	{"white", color.RGB{R: 255, G: 255, B: 255}, color.RGB{R: 255, G: 255, B: 255}},
	{"checker", color.RGB{R: 102, G: 102, B: 102}, color.RGB{R: 153, G: 153, B: 153}},
}

func (a *app) fillBackdrop(r tui.Region) {
	bd := backdrops[a.backdrop]
	for y := range r.H {
		for x := range r.W {
			c := bd.a
			if (x/2+y)%2 == 1 {
				c = bd.b
			}
			r.CellOpaque(x, y, ' ', a.pal.fg, c, terminal.AttrNone)
		}
	}
}

// drawFrame draws a frame of an image on the backdrop, its transparent cells
// left to show it
func (a *app) drawFrame(r tui.Region, s *source, f frame, w *window) {
	a.fillBackdrop(r)
	cells, rr := w.cellsFor(s, f, a.lk)
	x0, y0, rw := rr.Min.X-f.ox, rr.Min.Y-f.oy, rr.Dx()
	for i, c := range cells {
		if c.Rune != 0 {
			r.CellOpaque(x0+i%rw, y0+i/rw, c.Rune, c.Fg, c.Bg, c.Attrs)
		}
	}
}

// spinner is the frame a spinner shows now
func (a *app) spinner() int { return int(time.Since(a.start) / tickEvery) }

func (a *app) render() {
	w, h := a.w, a.h
	if len(a.cells) != w*h {
		a.cells = make([]terminal.Cell, w*h)
	}
	root := tui.NewRegion(a.cells, w, 0, 0, w, h)
	root.Fill(a.pal.bg)
	if w < 20 || h < 4 {
		root.Text(0, 0, tui.Truncate("too small: q quits", w), a.pal.warn, a.pal.bg, terminal.AttrBold)
		a.term.Flush(a.cells, w, h)
		return
	}
	if a.viewing != "" {
		a.renderView(root)
	} else {
		a.renderBrowse(root)
	}
	switch a.overlay {
	case ovHelp:
		a.renderHelp(root)
	case ovExport:
		a.ex.render(a, root)
	case ovConfirm:
		root.ConfirmDialog(a.confirm, tui.ConfirmOpts{Title: "Overwrite", Message: a.confirmMsg,
			YesLabel: "Overwrite", NoLabel: "Keep", Destructive: true})
	}
	if time.Now().Before(a.toastTill) {
		root.Toast(a.toast)
	}
	a.term.Flush(a.cells, w, h)
}

// renderBrowse lays the browser out by the room it has: list and preview side
// by side, or the preview under the list in a tall narrow pane, or one of
// them alone, Tab choosing which
func (a *app) renderBrowse(root tui.Region) {
	header, rest := tui.SplitVFixed(root, 1)
	body, footer := tui.SplitVFixed(rest, rest.H-1)
	a.renderHeader(header)
	a.geom.list, a.geom.preview = rect{}, rect{}
	switch {
	case body.W >= 64:
		list, preview := tui.SplitHFixed(body, max(22, min(48, body.W*34/100)))
		a.renderList(list)
		a.renderPreview(preview)
	case body.H >= 14:
		list, preview := tui.SplitVFixed(body, max(4, min(12, body.H*40/100)))
		a.renderList(list)
		a.renderPreview(preview)
	case a.showPreview:
		a.renderPreview(body)
	default:
		a.renderList(body)
	}
	if a.overlay == ovFilter {
		a.renderFilter(footer)
	} else {
		a.renderHints(footer, browseHints)
	}
}

var browseHints = [][2]string{
	{"↵", "open"}, {"h/l", "up/in"}, {"/", "filter"}, {"␣", "mark"}, {"w", "write"},
	{"s", "sort"}, {"m", "mode"}, {"c", "color"}, {"b", "backdrop"}, {"?", "keys"}, {"q", "quit"},
}

func (a *app) renderHeader(r tui.Region) {
	r.Fill(a.pal.bar)
	r.Text(0, 0, " ascimage ", a.pal.bg, a.pal.accent, terminal.AttrBold)
	pos := "0/0"
	if n := len(a.br.rows); n > 0 {
		pos = fmt.Sprintf("%d/%d", a.br.cursor+1, n)
	}
	hint := tui.Style{Fg: a.pal.dim}
	sections := []tui.BarSection{
		{Label: "sort ", Value: a.br.sortBy.String(), LabelStyle: hint, ValueStyle: tui.Style{Fg: a.pal.barFg}, Priority: 0},
		{Value: modeName(a.lk.mode), ValueStyle: tui.Style{Fg: a.pal.barFg}, Priority: 1},
		{Value: depthName(a.lk.depth), ValueStyle: tui.Style{Fg: a.pal.barFg}, Priority: 1},
		{Value: pos, ValueStyle: tui.Style{Fg: a.pal.fg, Attr: terminal.AttrBold}, Priority: 3},
	}
	if n := len(a.br.marks); n > 0 {
		sections = append(sections, tui.BarSection{Label: "marked ", Value: fmt.Sprint(n), LabelStyle: hint, ValueStyle: tui.Style{Fg: a.pal.ok}, Priority: 2})
	}
	bw := min(barWidth(sections), (r.W-10)/2)
	r.Sub(r.W-bw, 0, bw, 1).StatusBar(0, sections, tui.BarOpts{Bg: a.pal.bar, Align: tui.BarAlignRight})
	r.Text(11, 0, tailTruncate(a.br.dir, r.W-12-bw), a.pal.fg, a.pal.bar, terminal.AttrBold)
}

// barWidth is the row a status bar's sections take whole, its padding and
// separators included
func barWidth(sections []tui.BarSection) int {
	n := 2 + 3*(len(sections)-1)
	for _, s := range sections {
		n += tui.RuneLen(s.Label) + tui.RuneLen(s.Value)
	}
	return n
}

func (a *app) renderHints(r tui.Region, hints [][2]string) {
	r.Fill(a.pal.bar)
	x := 1
	for _, h := range hints {
		key, label := " "+h[0]+" ", h[1]+"  "
		if x+tui.RuneLen(key)+tui.RuneLen(label) > r.W {
			break
		}
		r.Text(x, 0, key, a.pal.bg, a.pal.accent, terminal.AttrBold)
		x += tui.RuneLen(key)
		r.Text(x, 0, label, a.pal.dim, a.pal.bar, terminal.AttrNone)
		x += tui.RuneLen(label)
	}
}

func (a *app) renderFilter(r tui.Region) {
	st := tui.DefaultTextFieldStyle()
	st.TextBg, st.PrefixFg, st.TextFg = a.pal.bar, a.pal.accent, a.pal.fg
	st.CursorBg, st.CursorFg = a.pal.fg, a.pal.bg
	r.TextField(a.filterTF, tui.TextFieldOpts{Prefix: "/", Border: tui.LineNone, Focused: true, Style: st, Placeholder: "filter names"})
}

func (a *app) renderList(r tui.Region) {
	b := &a.br
	title := filepath.Base(b.dir)
	if b.filter != "" {
		title += " /" + b.filter
	}
	in := r.Pane(tui.PaneOpts{Title: title, Border: tui.LineRounded, BorderFg: a.pal.border, TitleFg: a.pal.accent, Bg: a.pal.bg})
	if in.W < 4 || in.H < 1 {
		return
	}
	list := in.Sub(0, 0, in.W-1, in.H)
	a.geom.list = rectOf(list)
	b.scroll = tui.AdjustScroll(b.cursor, b.scroll, list.H, len(b.rows))
	switch {
	case b.err != nil:
		list.TextBlock(1, 0, b.err.Error(), a.pal.warn, a.pal.bg, terminal.AttrNone)
		return
	case len(b.rows) == 0 && b.filter != "":
		list.TextCenter(list.H/2, "nothing matches /"+b.filter, a.pal.dim, a.pal.bg, terminal.AttrNone)
		return
	case len(b.rows) == 0:
		list.TextCenter(list.H/2, "no images or folders", a.pal.dim, a.pal.bg, terminal.AttrNone)
		return
	}
	sizeW := 0
	if list.W >= 30 {
		sizeW = 9
	}
	for y := range list.H {
		row := b.scroll + y
		if row >= len(b.rows) {
			break
		}
		e := b.all[b.rows[row]]
		fg, attr, name := a.pal.fg, terminal.AttrNone, e.name
		switch {
		case e.dir:
			fg, attr, name = a.pal.accent, terminal.AttrBold, name+"/"
		case isAIMG(e.name):
			fg = a.pal.accent2
		}
		if strings.HasPrefix(e.name, ".") {
			attr |= terminal.AttrDim
		}
		bg, mark := a.pal.bg, ' '
		if row == b.cursor {
			bg, mark = a.pal.sel, '▸'
			if a.pal.reverse {
				attr |= terminal.AttrReverse
			}
		}
		for x := range list.W {
			list.CellOpaque(x, y, ' ', fg, bg, attr&terminal.AttrReverse)
		}
		list.Cell(0, y, mark, a.pal.accent, bg, attr&terminal.AttrReverse)
		if b.marks[e.name] {
			list.Cell(1, y, '●', a.pal.ok, bg, attr&terminal.AttrReverse)
		}
		list.Text(2, y, tui.Truncate(name, list.W-3-sizeW), fg, bg, attr)
		if sizeW > 0 && !e.dir {
			sz, szFg := humanSize(e.size), a.pal.dim
			if attr&terminal.AttrReverse != 0 {
				szFg = fg // reversed, dim would read as a second bar
			}
			list.Text(list.W-tui.RuneLen(sz)-1, y, sz, szFg, bg, attr&terminal.AttrReverse)
		}
	}
	if len(b.rows) > list.H {
		bar := in.Sub(in.W-1, 0, 1, in.H)
		bar.ScrollBar(0, b.scroll, bar.H, len(b.rows), a.pal.border)
	}
}

func (a *app) renderPreview(r tui.Region) {
	e, ok := a.cursorEntry()
	title := "preview"
	if ok {
		title = e.name
	}
	in := r.Pane(tui.PaneOpts{Title: title, Border: tui.LineSingle, BorderFg: a.pal.border, TitleFg: a.pal.dim, Bg: a.pal.bg})
	if in.W < 2 || in.H < 1 {
		return
	}
	a.geom.preview = rectOf(in)
	if !ok {
		return
	}
	if e.dir {
		in.TextCenter(in.H/2, "a folder: ↵ opens it", a.pal.dim, a.pal.bg, terminal.AttrNone)
		return
	}
	area, info := in, tui.Region{}
	if in.H >= 3 {
		area, info = tui.SplitVFixed(in, in.H-1)
	}
	s, err := a.source(a.pathOf(e))
	switch {
	case err != nil:
		in.TextBlock(1, 0, errText(a.pathOf(e), err), a.pal.warn, a.pal.bg, terminal.AttrNone)
	case s == nil:
		in.Spinner(in.W/2-5, in.H/2, a.spinner(), a.pal.accent2)
		in.Text(in.W/2-3, in.H/2, "decoding", a.pal.dim, a.pal.bg, terminal.AttrNone)
	default:
		v := newView(true, 100)
		f := v.frame(s, area.W, area.H, a.lk)
		a.drawFrame(area, s, f, &a.pwin)
		info.Text(0, 0, tui.Truncate(a.describe(s, f), info.W), a.pal.dim, a.pal.bg, terminal.AttrNone)
	}
}

// describe is a source shown at a frame, for a status line
func (a *app) describe(s *source, f frame) string {
	at := fmt.Sprintf("%dx%d cells at %d%%", f.gw, f.gh, f.percent(s, a.lk))
	if s.dual != nil {
		return fmt.Sprintf("%dx%d cells .%s %s · %s · %s", s.w, s.h, s.format, modeName(s.dual.RenderMode), humanSize(s.size), at)
	}
	return fmt.Sprintf("%dx%d %s · %s · %s", s.w, s.h, strings.ToUpper(s.format), humanSize(s.size), at)
}

func (a *app) renderView(root tui.Region) {
	area, bar := root, tui.Region{}
	if a.status {
		area, bar = tui.SplitVFixed(root, root.H-1)
	}
	s, err := a.source(a.viewing)
	var f frame
	switch {
	case err != nil:
		area.TextBlock(1, area.H/2, errText(a.viewing, err), a.pal.warn, a.pal.bg, terminal.AttrNone)
	case s == nil:
		area.Spinner(area.W/2-5, area.H/2, a.spinner(), a.pal.accent2)
		area.Text(area.W/2-3, area.H/2, "decoding", a.pal.dim, a.pal.bg, terminal.AttrNone)
	default:
		f = a.vw.frame(s, area.W, area.H, a.lk)
		a.drawFrame(area, s, f, &a.vwin)
	}
	if !a.status {
		return
	}
	bar.Fill(a.pal.bar)
	hint := tui.Style{Fg: a.pal.dim}
	val := tui.Style{Fg: a.pal.barFg}
	sections := []tui.BarSection{{Value: filepath.Base(a.viewing), ValueStyle: tui.Style{Fg: a.pal.fg, Attr: terminal.AttrBold}, Priority: 4}}
	if rows := a.imageRows(); a.viewingRow() >= 0 {
		k := slices.Index(rows, a.viewingRow())
		sections = append(sections, tui.BarSection{Value: fmt.Sprintf("%d/%d", k+1, len(rows)), ValueStyle: val, Priority: 2})
	}
	if s != nil {
		zoom := fmt.Sprintf("%d%%", f.percent(s, a.lk))
		if a.vw.fit {
			zoom = "fit " + zoom
		}
		dims := fmt.Sprintf("%dx%d → %dx%d", s.w, s.h, f.gw, f.gh)
		mode := modeName(a.lk.mode)
		if s.dual != nil {
			dims, mode = fmt.Sprintf("%dx%d cells", s.w, s.h), modeName(s.dual.RenderMode)
		}
		sections = append(sections,
			tui.BarSection{Value: dims, ValueStyle: val, Priority: 1},
			tui.BarSection{Value: zoom, ValueStyle: tui.Style{Fg: a.pal.accent}, Priority: 3},
			tui.BarSection{Value: mode, ValueStyle: val, Priority: 1},
			tui.BarSection{Value: depthName(a.lk.depth), ValueStyle: val, Priority: 1})
		if f.overflows() {
			sections = append(sections, tui.BarSection{Label: "at ", Value: fmt.Sprintf("%d,%d", max(0, f.ox), max(0, f.oy)), LabelStyle: hint, ValueStyle: val, Priority: 0})
		}
	}
	sections = append(sections, tui.BarSection{Label: "? ", Value: "keys", LabelStyle: tui.Style{Fg: a.pal.accent}, ValueStyle: hint, Priority: 2})
	bar.StatusBar(0, sections, tui.BarOpts{Bg: a.pal.bar, Align: tui.BarAlignLeft})
}

var (
	browseKeys = [][2]string{
		{"j k ↑ ↓", "move"}, {"l → ↵", "open the folder or image"}, {"h ← ⌫", "the folder above"},
		{"gg G", "first, last"}, {"PgUp PgDn ^U ^D", "a page, half a page"}, {"/", "filter names; Esc clears"},
		{"␣ a u", "mark, mark all, clear marks"}, {"w", "write the marked or the cursor's images"},
		{"s . ~", "sort, hidden files, home folder"}, {"Tab", "list or preview, when only one fits"},
	}
	viewKeys = [][2]string{
		{"h j k l ←↓↑→", "pan; H J K L and ⇧ by 10"}, {"^U ^D PgUp PgDn", "half a screen, a screen"},
		{"g G Home End", "top, bottom, left, right"}, {"+ - 0 f", "zoom in, out, 1:1, fit"},
		{"n ␣ ] / p ⌫ [", "next, previous image"}, {"w", "write this image as shown"},
		{"s", "status bar"}, {"Esc", "back to the folder"},
		{"mouse", "wheel pans, ^wheel zooms, drag pans"},
	}
	lookKeys = [][2]string{
		{"m", "quadrant or background cells"}, {"c", "color: true, 256, 16"},
		{"b", "backdrop: panel, black, white, checker"}, {"? ^L q", "keys, redraw, quit"},
	}
)

func (a *app) renderHelp(root tui.Region) {
	keys := browseKeys
	title := "ascimage: browsing"
	if a.viewing != "" {
		keys, title = viewKeys, "ascimage: viewing"
	}
	keys = append(append([][2]string{}, keys...), lookKeys...)
	box := tui.Center(root, min(64, root.W-2), min(len(keys)+4, root.H-2))
	in := box.Modal(tui.ModalOpts{Title: title, Hint: "any key closes", Border: tui.LineRounded,
		BorderFg: a.pal.accent, TitleFg: a.pal.fg, HintFg: a.pal.dim, Bg: a.pal.bar})
	ks := tui.Style{Fg: a.pal.accent, Bg: a.pal.bar, Attr: terminal.AttrBold}
	vs := tui.Style{Fg: a.pal.fg, Bg: a.pal.bar}
	for i, kv := range keys {
		in.KeyValueColumn(i+1, 18, kv[0], kv[1], ks, vs, ' ')
	}
}

// errText is a source's error without its path, which the pane names
func errText(path string, err error) string {
	return strings.TrimPrefix(err.Error(), path+": ")
}

func tailTruncate(s string, w int) string {
	if w < 4 {
		return ""
	}
	if rs := []rune(s); len(rs) > w {
		return "…" + string(rs[len(rs)-w+1:])
	}
	return s
}
