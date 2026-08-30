package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif-log/internal/keys"
	"github.com/lixenwraith/vif-log/internal/logfile"
	"github.com/lixenwraith/vif-log/internal/ui"
)

// paneOrder fixes render order. The list sizes itself first so the status bar
// reads that height in the same frame.
var paneOrder = []ui.Pane{
	ui.PaneList, ui.PaneDetail, ui.PaneHeader, ui.PaneStatus, ui.PaneFooter,
}

// renderers binds panes to draw functions; layout decides where they land.
var renderers = map[ui.Pane]func(*App, tui.Region){
	ui.PaneHeader: (*App).renderHeader,
	ui.PaneList:   (*App).renderList,
	ui.PaneDetail: (*App).renderDetail,
	ui.PaneStatus: (*App).renderStatus,
	ui.PaneFooter: (*App).renderFooter,
}

// Render draws one frame.
func (a *App) Render() {
	w, h := a.w, a.h
	if w < 4 || h < 2 {
		return
	}
	if len(a.cells) != w*h {
		a.cells = make([]terminal.Cell, w*h)
	}
	blank := terminal.Cell{Rune: ' ', Fg: a.th.Fg, Bg: a.th.Bg}
	for i := range a.cells {
		a.cells[i] = blank
	}
	cells := a.cells
	root := tui.NewRegion(cells, w, 0, 0, w, h)

	if w < minW || h < minH {
		a.renderTooSmall(root)
		a.term.Flush(cells, w, h)
		return
	}

	regions := a.lay.Resolve(root)
	for _, pane := range paneOrder {
		if fn, region := renderers[pane], regions[pane]; fn != nil && region.W > 0 {
			fn(a, region)
		}
	}

	switch a.overlay {
	case ovPrompt:
		a.renderPrompt(regions[ui.PaneFooter])
	case ovHelp:
		a.renderHelp(root)
	case ovOpen:
		a.renderBrowser(root)
	}
	if a.toast.Visible {
		root.Toast(a.toast.Opts)
	}
	a.term.Flush(cells, w, h)
}

// renderTooSmall reports the shortfall rather than degrading the layout.
func (a *App) renderTooSmall(r tui.Region) {
	r.Fill(a.th.Bg)
	y := r.H / 2
	r.TextCenter(y-1, "terminal too small", a.th.Warning, a.th.Bg, terminal.AttrBold)
	r.TextCenter(y+1, fmt.Sprintf("%dx%d - need %dx%d", a.w, a.h, minW, minH),
		a.th.HintFg, a.th.Bg, terminal.AttrNone)
	r.TextCenter(y+3, "q quits", a.th.HintFg, a.th.Bg, terminal.AttrDim)
}

// --- bars ------------------------------------------------------------------

// pair is a label/value cell in the header and status bars.
type pair struct {
	label, value string
	fg           color.RGB
}

const pairSep = " · "
const pairSepW = 3 // runes; len(pairSep) is 4 bytes

// drawPairsRight renders pairs right-aligned, dropping leading pairs that do
// not fit. Returns the x where drawing started, so the caller can clip its own
// left-aligned content. Unlike tui.StatusBar this does not clear the row.
func (a *App) drawPairsRight(r tui.Region, y int, ps []pair, bg color.RGB) int {
	width := func(p pair) int { return tui.RuneLen(p.label) + 1 + tui.RuneLen(p.value) }

	total := 0
	for i, p := range ps {
		total += width(p)
		if i > 0 {
			total += pairSepW
		}
	}
	for len(ps) > 1 && total > r.W-2 {
		total -= width(ps[0]) + pairSepW
		ps = ps[1:]
	}

	x := max(0, r.W-total-1)
	start := x
	for i, p := range ps {
		if i > 0 {
			r.Text(x, y, pairSep, a.th.HintFg, bg, terminal.AttrDim)
			x += pairSepW
		}
		r.Text(x, y, p.label, a.th.HintFg, bg, terminal.AttrNone)
		x += tui.RuneLen(p.label) + 1
		r.Text(x, y, p.value, p.fg, bg, terminal.AttrBold)
		x += tui.RuneLen(p.value)
	}
	return start
}

func (a *App) renderHeader(r tui.Region) {
	r.Fill(a.th.HeaderBg)
	rx := a.drawPairsRight(r, 0, a.headerPairs(), a.th.HeaderBg)

	x := 1
	r.Text(x, 0, " vif-log ", a.th.Bg, a.th.Accent, terminal.AttrBold)
	x += 10
	if rx > x+2*int(logfile.LevelCount) {
		x = a.drawLevelStrip(r, x)
	}
	if w := rx - x - 2; w > 8 {
		// title carries "name +N" once several files are merged
		name := a.title
		if name == "" {
			name = "no file - press o to open"
		}
		r.Text(x+1, 0, tui.TruncateLeft(name, w), a.th.HeaderFg, a.th.HeaderBg, terminal.AttrBold)
	}
}

// drawLevelStrip shows every level's initial, filled for the levels currently
// shown and outlined for the hidden ones. Returns the x past the strip.
func (a *App) drawLevelStrip(r tui.Region, x int) int {
	for l := logfile.Level(0); l < logfile.LevelCount; l++ {
		fg, bg, attr := a.th.Level[l], a.th.HeaderBg, terminal.AttrNone
		if a.lvl.Has(l) {
			fg, bg, attr = a.th.HeaderBg, a.th.Level[l], terminal.AttrBold
		}
		r.Cell(x, 0, rune(l.Initial()), fg, bg, attr)
		x += 2
	}
	return x
}

func (a *App) headerPairs() []pair {
	ps := make([]pair, 0, 4)
	if scanned, total, done := a.idx.Progress(); !done {
		p := 0
		if total > 0 {
			p = int(scanned * 100 / total)
		}
		ps = append(ps, pair{"indexing", fmt.Sprintf("%d%%", p), a.th.Accent2})
	}
	n := len(a.rows())
	row := "0"
	if n > 0 {
		row = fmt.Sprint(a.cursor + 1)
	}
	return append(ps,
		pair{"row", row, a.th.HeaderFg},
		pair{"shown", fmt.Sprint(n), a.th.Selected},
		pair{"total", fmt.Sprint(a.idx.Len()), a.th.StatusFg},
	)
}

func (a *App) renderStatus(r tui.Region) {
	r.Fill(a.th.HeaderBg)
	rx := a.drawPairsRight(r, 0, a.statusPairs(), a.th.HeaderBg)
	if s := strings.Join(a.stack.Summary(), "  "); s != "" && rx > 3 {
		r.Text(1, 0, tui.Truncate(s, rx-2), a.th.Accent, a.th.HeaderBg, terminal.AttrBold)
	}
}

// statusPairs is ordered least to most important: overflow drops from the left.
func (a *App) statusPairs() []pair {
	ps := make([]pair, 0, 5)
	if a.build.busy {
		ps = append(ps, pair{"filtering",
			fmt.Sprintf("%d%%", pct(a.build.next, a.idx.Len())), a.th.Accent2})
	}
	if n := a.idx.Malformed(); n > 0 {
		ps = append(ps, pair{"bad", fmt.Sprint(n), a.th.Error})
	}
	if n := a.pins.Len(); n > 0 {
		ps = append(ps, pair{"pins", fmt.Sprint(n), a.th.Warning})
	}
	if a.sortDir != sortNone {
		ps = append(ps, pair{"sort", a.sortCol.String() + " " + a.sortDir.String(), a.th.Partial})
	}
	return append(ps, pair{"col", a.col.String(), a.th.Accent})
}

func pct(n, total int) int {
	if total <= 0 {
		return 100
	}
	return n * 100 / total
}

// --- record list -----------------------------------------------------------

// colLayout is the physical geometry of the list pane: pin gutter, optional
// source gutter, then the focusable columns with level wedged after time.
type colLayout struct {
	w            int
	pinX         int
	srcX, srcW   int
	tsX, tsW     int
	lvlX         int
	tickX, tickW int
	subX, subW   int
	markX        int
	msgX, msgW   int
	fldX         int
}

func listCols(w, nsrc int) colLayout {
	c := colLayout{w: w, tsW: 12, subW: 8, msgW: 16}
	if nsrc > 1 {
		c.srcW = 1
	}
	if w >= 96 {
		c.tickW = 6
	}
	if w < 76 {
		c.msgW = 12
	}
	if w < 60 {
		c.subW, c.msgW = 6, 10
	}
	x := 0
	c.pinX, x = x, x+2
	c.srcX = x
	if c.srcW > 0 {
		x += c.srcW + 1
	}
	c.tsX, x = x, x+c.tsW+1
	c.lvlX, x = x, x+2
	c.tickX = x
	if c.tickW > 0 {
		x += c.tickW + 1
	}
	c.subX, x = x, x+c.subW+1
	c.markX, x = x, x+2
	c.msgX, x = x, x+c.msgW+1
	c.fldX = x
	return c
}

// span returns the header extent of a focusable column.
func (c colLayout) span(col logfile.Column) (int, int) {
	switch col {
	case logfile.ColTime:
		return c.tsX, c.tsW
	case logfile.ColTick:
		return c.tickX, c.tickW
	case logfile.ColSub:
		return c.subX, c.subW
	case logfile.ColMsg:
		return c.msgX, c.msgW
	case logfile.ColFields:
		return c.fldX, max(0, c.w-c.fldX)
	}
	return 0, c.w
}

func (a *App) renderList(r tui.Region) {
	r.Fill(a.th.Bg)
	list := r.Sub(0, 0, r.W-1, r.H)
	c := listCols(list.W, a.idx.SrcCount())
	a.renderColHeader(list, c)

	body := list.Sub(0, 1, list.W, list.H-1)
	a.listH = body.H
	a.clamp()

	rows := a.rows()
	if len(rows) == 0 {
		msg := "no records match"
		if a.idx == nil {
			msg = "no file open — press o"
		}
		body.TextCenter(body.H/2, msg, a.th.HintFg, a.th.Bg, terminal.AttrDim)
	} else {
		metas := a.idx.Metas()
		for y := 0; y < body.H; y++ {
			i := a.scroll + y
			if i < 0 || i >= len(rows) {
				break
			}
			rec := rows[i]
			if int(rec) >= len(metas) {
				break
			}
			a.renderRow(body, y, rec, metas[rec], c, i == a.cursor)
		}
	}

	sb := r.Sub(r.W-1, 1, 1, r.H-1)
	sb.ScrollBar(0, a.scroll, sb.H, len(rows), a.th.Border)
}

// renderColHeader names the columns and marks the focused one, which is both
// the search scope and the sort target.
func (a *App) renderColHeader(r tui.Region, c colLayout) {
	for x := 0; x < r.W; x++ {
		r.Cell(x, 0, ' ', a.th.HintFg, a.th.HeaderBg, terminal.AttrNone)
	}
	fx, fw := c.span(a.col)
	for x := fx; x < fx+fw && x < r.W; x++ {
		r.Cell(x, 0, ' ', a.th.Bg, a.th.Accent, terminal.AttrNone)
	}

	head := func(x, w int, s string, focused bool) {
		fg, bg := a.th.HintFg, a.th.HeaderBg
		if focused {
			fg, bg = a.th.Bg, a.th.Accent
		}
		r.Text(x, 0, tui.Truncate(s, max(1, w)), fg, bg, terminal.AttrBold)
	}
	all := a.col == logfile.ColAll
	head(c.tsX, c.tsW, "time", all || a.col == logfile.ColTime)
	head(c.lvlX, 1, "T", all)
	if c.tickW > 0 {
		head(c.tickX, c.tickW, "tick", all || a.col == logfile.ColTick)
	}
	head(c.subX, c.subW, "sub", all || a.col == logfile.ColSub)
	head(c.msgX, c.msgW, "msg", all || a.col == logfile.ColMsg)
	head(c.fldX, max(1, r.W-c.fldX), "fields", all || a.col == logfile.ColFields)

	if a.sortDir != sortNone && sortable(a.sortCol) {
		sx, sw := c.span(a.sortCol)
		if x := sx + sw - 1; sw > 0 && x < r.W {
			fg, bg := a.th.Accent, a.th.HeaderBg
			if all || a.sortCol == a.col {
				fg, bg = a.th.Bg, a.th.Accent
			}
			r.Cell(x, 0, a.sortDir.arrow(), fg, bg, terminal.AttrBold)
		}
	}
}

func (a *App) renderRow(r tui.Region, y int, rec int32, m logfile.Meta, c colLayout, cursor bool) {
	pinned := a.pins.Has(rec)
	bg := a.th.Bg
	switch {
	case cursor:
		bg = a.th.CursorBg
	case pinned:
		bg = a.th.FocusBg
	}
	for x := 0; x < r.W; x++ {
		r.Cell(x, y, ' ', a.th.Fg, bg, terminal.AttrNone)
	}
	if pinned {
		r.Cell(c.pinX, y, '◆', a.th.Warning, bg, terminal.AttrBold)
	}

	collapsed := m.Flags&logfile.FlagSnapHead != 0 && !a.snap.Expanded(m.Snap)

	// Source gutter: present only when more than one file is indexed
	if c.srcW > 0 {
		r.Cell(c.srcX, y, a.idx.SrcMark(m.Src), a.th.SrcColor(int(m.Src)), bg, terminal.AttrBold)
	}

	r.Text(c.tsX, y, tui.Truncate(logfile.StampText(m), c.tsW), a.th.HintFg, bg, terminal.AttrDim)

	lvlFg := a.th.Level[logfile.LevelBad]
	if m.Lvl < logfile.LevelCount {
		lvlFg = a.th.Level[m.Lvl]
	}
	r.Cell(c.lvlX, y, rune(m.Lvl.Initial()), lvlFg, bg, terminal.AttrBold)

	// Tick column appears only on wide terminals; the index carries it always
	if c.tickW > 0 {
		r.Text(c.tickX, y, tui.PadLeft(strconv.FormatUint(uint64(m.Tick), 10), c.tickW),
			a.th.HintFg, bg, terminal.AttrDim)
	}

	sub := logfile.Dash(a.idx.SubName(m.Sub))
	r.Text(c.subX, y, tui.PadRight(tui.Truncate(sub, c.subW), c.subW), a.th.SubColor(sub), bg, terminal.AttrNone)

	if mark := snapMark(m, collapsed); mark != 0 {
		r.Cell(c.markX, y, mark, a.th.SnapFg, bg, terminal.AttrNone)
	}

	msg, msgAttr := logfile.Dash(a.idx.MsgName(m.Msg)), terminal.AttrNone
	if collapsed {
		msg, msgAttr = "snapshot", terminal.AttrBold
	}
	r.Text(c.msgX, y, tui.PadRight(tui.Truncate(msg, c.msgW), c.msgW), a.th.Fg, bg, msgAttr)

	if c.fldX >= r.W {
		return
	}
	switch {
	case m.Flags&logfile.FlagMalformed != 0:
		r.Text(c.fldX, y, tui.Truncate("<malformed line>", r.W-c.fldX),
			a.th.Level[logfile.LevelBad], bg, terminal.AttrItalic)
	case collapsed:
		r.Text(c.fldX, y, tui.Truncate(a.snapSummary(m), r.W-c.fldX), a.th.SnapFg, bg, terminal.AttrNone)
	default:
		r.Text(c.fldX, y, tui.Truncate(a.summary(m), r.W-c.fldX), a.th.StatusFg, bg, terminal.AttrNone)
	}
}

// snapMark returns the group indicator: collapsed head, expanded head, member.
func snapMark(m logfile.Meta, collapsed bool) rune {
	switch {
	case m.Flags&logfile.FlagSnapHead != 0 && collapsed:
		return '▶'
	case m.Flags&logfile.FlagSnapHead != 0:
		return '▼'
	case m.Snap != 0:
		return '·'
	}
	return 0
}

// snapSummary describes a collapsed group from the index alone, no line read.
func (a *App) snapSummary(m logfile.Meta) string {
	if s, ok := a.idx.SnapshotOf(m); ok {
		return fmt.Sprintf("stat snapshot ×%d   run=%d tick=%d", s.Count, s.Run, s.Tick)
	}
	return fmt.Sprintf("stat snapshot       run=%d tick=%d", m.Run, m.Tick)
}

// summary renders the fields column; the search filter matches this same text.
func (a *App) summary(m logfile.Meta) string {
	line, err := a.rd.Line(m)
	if err != nil {
		return ""
	}
	a.rec.Parse(m, line)
	return a.rec.FieldsText()
}

// --- detail ----------------------------------------------------------------

type detailRow struct {
	k, v string
	head bool
}

func (a *App) renderDetail(r tui.Region) {
	r.Fill(a.th.Bg)
	r.VLine(0, tui.LineSingle, a.th.Border)
	c := r.Sub(2, 0, r.W-2, r.H)
	if c.W < 8 || c.H < 2 {
		return
	}

	rec := a.cursorRec()
	m, ok := a.meta(rec)
	if !ok {
		c.TextCenter(0, "no record", a.th.HintFg, a.th.Bg, terminal.AttrDim)
		return
	}
	line, err := a.rd.Line(m)
	if err != nil {
		c.Text(0, 0, "read: "+err.Error(), a.th.Error, a.th.Bg, terminal.AttrNone)
		return
	}
	a.rec.Parse(m, line)

	rows := make([]detailRow, 0, 10+len(a.rec.Fields))

	appendWrapped := func(key, val string, head bool) string {
		if val == "" {
			rows = append(rows, detailRow{key, "", head})
			return " "
		}
		for _, vline := range strings.Split(val, "\n") {
			runes := []rune(vline)
			if len(runes) == 0 {
				rows = append(rows, detailRow{key, "", head})
				key = " "
				continue
			}
			for len(runes) > 0 {
				width := max(10, c.W-tui.RuneLen(key)-2)
				chunk := runes
				if len(chunk) > width {
					chunk = chunk[:width]
				}
				rows = append(rows, detailRow{key, string(chunk), head})
				runes = runes[len(chunk):]
				key = " "
			}
		}
		return key
	}

	appendWrapped("time", a.rec.Time, true)
	appendWrapped("level", m.Lvl.String(), true)
	// Source line only earns its row in a merged view
	if a.idx.SrcCount() > 1 {
		appendWrapped("file", a.idx.SrcName(m.Src), true)
	}
	appendWrapped("sub", logfile.Dash(a.idx.SubName(m.Sub)), true)
	appendWrapped("run", fmt.Sprint(m.Run), true)
	appendWrapped("tick", fmt.Sprint(m.Tick), true)
	appendWrapped("frame", fmt.Sprint(m.Frame), true)

	if a.rec.Trace != "" {
		key := "trace"
		steps := strings.Split(a.rec.Trace, " -> ")
		for i, step := range steps {
			if i > 0 {
				step = "-> " + step
			}
			key = appendWrapped(key, step, true)
		}
	}
	if s, ok := a.idx.SnapshotOf(m); ok {
		appendWrapped("snapshot", fmt.Sprintf("%d records, head %d", s.Count, s.Head), true)
	}
	if a.pins.Has(rec) {
		appendWrapped("pinned", "yes", true)
	}
	rows = append(rows, detailRow{})

	if a.rec.Bad {
		raw := strings.TrimRight(string(line), "\r\n")
		appendWrapped("raw", raw, false)
	} else {
		for _, f := range a.rec.Fields {
			appendWrapped(f.Key, a.rec.Display(f), false)
		}
	}

	ks := tui.Style{Fg: a.th.KeyFg}
	a.dscroll = tui.ClampScroll(a.dscroll, c.H, len(rows))
	for y := 0; y < c.H; y++ {
		i := a.dscroll + y
		if i >= len(rows) {
			break
		}
		if rows[i].k == "" {
			c.HLine(y, tui.LineSingle, a.th.Border)
			continue
		}
		vs := tui.Style{Fg: a.th.StrFg}
		if rows[i].head {
			vs = tui.Style{Fg: a.th.Fg}
		}
		c.KeyValue(y, rows[i].k, rows[i].v, ks, vs, ':')
	}
}

// --- footer / prompt -------------------------------------------------------

// footerActions lists the bindings specific to this viewer; generic movement
// and quit keys live in the help overlay. Chords come from the key table.
var footerActions = []struct {
	act   keys.Action
	label string
}{
	{keys.ActHelp, "help"},
	{keys.ActOpen, "open"},
	{keys.ActSearch, "find"},
	{keys.ActFilter, "filter"},
	{keys.ActFollowNext, "same"},
	{keys.ActExpand, "snap"},
	{keys.ActPinToggle, "pin"},
	{keys.ActPinOnly, "only"},
	{keys.ActExport, "export"},
	{keys.ActColNext, "col"},
	{keys.ActSort, "sort"},
}

func (a *App) renderFooter(r tui.Region) {
	r.Fill(a.th.HeaderBg)
	x := 1
	for _, h := range footerActions {
		k := " " + keys.KeysFor(keys.ModeNormal, h.act) + " "
		l := h.label + " "
		if x+tui.RuneLen(k)+tui.RuneLen(l) > r.W {
			break
		}
		r.Text(x, 0, k, a.th.Bg, a.th.Accent, terminal.AttrBold)
		x += tui.RuneLen(k)
		r.Text(x, 0, l, a.th.HintFg, a.th.HeaderBg, terminal.AttrNone)
		x += tui.RuneLen(l)
	}
}

// renderPrompt replaces the footer row while text is being entered.
func (a *App) renderPrompt(r tui.Region) {
	if r.W < 5 || r.H < 1 || a.prompt == nil {
		return
	}
	st := tui.DefaultTextFieldStyle()
	st.TextBg, st.PrefixFg, st.TextFg = a.th.InputBg, a.th.Accent, a.th.Fg
	st.CursorBg, st.CursorFg = a.th.Fg, a.th.Bg
	r.TextField(a.prompt, tui.TextFieldOpts{
		Prefix: a.promptKind.prefix(a.col), Border: tui.LineNone,
		Focused: true, Style: st,
	})
}

// --- help ------------------------------------------------------------------

// helpLine is one rendered row of the help panel: a group heading or a binding.
type helpLine struct {
	keys, text string
	head       bool
}

// buildHelpLines flattens the key table once at startup.
func buildHelpLines() []helpLine {
	var out []helpLine
	group := ""
	for _, m := range []keys.Mode{keys.ModeNormal, keys.ModeOverlay} {
		for _, row := range keys.HelpRows(m) {
			if row.Group != group {
				group = row.Group
				if len(out) > 0 {
					out = append(out, helpLine{})
				}
				out = append(out, helpLine{text: strings.ToUpper(group), head: true})
			}
			out = append(out, helpLine{keys: row.Keys, text: row.Help})
		}
	}
	return out
}

func (a *App) renderHelp(root tui.Region) {
	a.help.Rows = len(a.helpLines)
	a.help.Title = "vif-log keys"
	a.help.Hint = "↑↓ scroll   esc close"

	body := a.help.Render(root, &a.th)
	if body.W < 10 || body.H < 1 {
		return
	}
	ks := tui.Style{Fg: a.th.Accent, Bg: a.th.FocusBg, Attr: terminal.AttrBold}
	vs := tui.Style{Fg: a.th.Fg, Bg: a.th.FocusBg}
	for y := 0; y < body.H; y++ {
		i := a.help.Scroll + y
		if i >= len(a.helpLines) {
			break
		}
		l := a.helpLines[i]
		switch {
		case l.head:
			body.Text(1, y, l.text, a.th.Accent2, a.th.FocusBg, terminal.AttrBold)
		case l.text != "":
			body.KeyValue(y, l.keys, l.text, ks, vs, ' ')
		}
	}
}
