//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

// Data and render side of the TUI: log capture, browser lists, the
// reflection-built inspector tree, header/footer. The inspector walks the
// same toml key space path.go addresses, so what it shows is exactly what
// set/add/del/unset can reach.

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/pkg/audio"
)

// --- log capture ---

// LogBuffer captures what a session says for the log pane, the last max lines.
// Event-loop confined: every Execute call and every render runs there, so no
// lock. A partial line (no trailing newline) is held until completed.
type LogBuffer struct {
	lines []string
	part  string
	max   int
}

func NewLogBuffer(max int) *LogBuffer { return &LogBuffer{max: max} }

// Lines is what the buffer holds, for a host whose screen never opened
func (l *LogBuffer) Lines() []string { return slices.Clone(l.lines) }

func (l *LogBuffer) Write(p []byte) (int, error) {
	l.part += string(p)
	for {
		i := strings.IndexByte(l.part, '\n')
		if i < 0 {
			break
		}
		l.push(l.part[:i])
		l.part = l.part[i+1:]
	}
	return len(p), nil
}

func (l *LogBuffer) push(s string) {
	l.lines = append(l.lines, s)
	if n := len(l.lines) - l.max; n > 0 {
		l.lines = slices.Delete(l.lines, 0, n)
	}
}

// --- selection ---

func (a *tuiApp) browserNames() []string {
	switch a.kind {
	case kindSound:
		return a.s.sounds.order
	case kindVoice:
		return a.s.voices.order
	}
	return a.s.pats.order
}

func (a *tuiApp) selName() string {
	ns := a.browserNames()
	c := a.brCursor[a.kind]
	if c < 0 || c >= len(ns) {
		return ""
	}
	return ns[c]
}

// addressableSel is the selection when the command grammar can name it
func (a *tuiApp) addressableSel() (string, bool) {
	n := a.selName()
	if n == "" {
		return "", false
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return "", false
	}
	return n, true
}

// addressable rejects names the command grammar cannot express. Creation
// guards them out (badName); loaded files can still contain them, so the
// browser shows such entries but macro keys refuse to build commands.
func addressable(n string) bool { return n != "" && !strings.ContainsAny(n, " \t.#") }

// cursorTo lands the active tab's cursor on name; false when absent (the
// command that should have created it failed).
func (a *tuiApp) cursorTo(name string) bool {
	i := slices.Index(a.browserNames(), name)
	if i < 0 {
		return false
	}
	a.brCursor[a.kind] = i
	return true
}

// --- inspector tree ---

// nodeMeta rides TreeNode.Data so key handlers know what is legal at the
// cursor without re-walking the spec.
type nodeMeta struct {
	segs  []string
	leaf  bool
	slice bool // slice field node (including empty)
	elem  bool // slice element node
	ro    bool // top-level name: registration identity, mv only
}

func (a *tuiApp) curMeta() (*nodeMeta, string) {
	if a.tree.Cursor < 0 || a.tree.Cursor >= len(a.nodes) {
		return nil, ""
	}
	n := &a.nodes[a.tree.Cursor]
	return n.Data.(*nodeMeta), n.Key
}

func (a *tuiApp) curExpandable() bool {
	if a.tree.Cursor < 0 || a.tree.Cursor >= len(a.nodes) {
		return false
	}
	return a.nodes[a.tree.Cursor].Expandable
}

func (a *tuiApp) buildNodes(name string) {
	a.nodes = a.nodes[:0]
	root, _, err := a.s.docRoot(name)
	if err != nil {
		return
	}
	a.walkStruct(name, reflect.ValueOf(root).Elem(), nil, 0, false)
}

// walkStruct emits one node per toml key, recursing into expanded
// composites. keysOf is alphabetical, matching MarshalSounds' key order, so
// the inspector and a saved file read the same way. omitempty is read here:
// it is the exact serialization rule that makes a zero value mean "default".
func (a *tuiApp) walkStruct(name string, sv reflect.Value, segs []string, depth int, unset bool) {
	t := sv.Type()
	idx := tagIndex(t)
	for _, key := range keysOf(t) {
		f := t.Field(idx[key])
		oe := strings.Contains(f.Tag.Get("toml"), "omitempty")
		a.emitField(name, key, sv.Field(idx[key]), append(slices.Clone(segs), key), depth, unset, oe)
	}
}

// emitField renders one field. A nil pointer sub-table (Burst, Vibrato) is walked
// through its zero value so fields that do not exist yet can be browsed and edited
// (set's create-on-write resolve allocates on commit); the substituted subtree
// renders dimmed, as does a zero omitempty leaf, which a save omits as a default.
func (a *tuiApp) emitField(name, key string, fv reflect.Value, segs []string, depth int, unset, omitEmpty bool) {
	full := name + "." + strings.Join(segs, ".")
	for fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			unset = true
			fv = reflect.Zero(fv.Type().Elem())
			continue
		}
		fv = fv.Elem()
	}
	st := tui.Style{Fg: a.theme.Fg}
	if unset {
		st.Fg = a.theme.HintFg
	}
	sufSt := tui.Style{Fg: a.theme.HintFg}

	switch fv.Kind() {
	case reflect.Struct:
		exp := a.exp.IsExpanded(full)
		suf := summarize(fv)
		if unset {
			suf = "(unset)"
		}
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: key, Depth: depth,
			Expandable: true, Expanded: exp,
			Style: st, Suffix: suf, SuffixStyle: sufSt,
			Data: &nodeMeta{segs: segs},
		})
		if exp {
			a.walkStruct(name, fv, segs, depth+1, unset)
		}
	case reflect.Slice:
		n := fv.Len()
		exp := a.exp.IsExpanded(full) && n > 0
		// A list of names shows them; anything else its count, "(n)" not "[n]":
		// bus[1] read as "routed to bus 1" when it meant "one bus defined".
		suf := fmt.Sprintf("(%d)", n)
		if fv.Type().Elem().Kind() == reflect.String && n > 0 {
			words := make([]string, n)
			for i := range n {
				words[i] = fv.Index(i).String()
			}
			suf = "= " + strings.Join(words, " ")
		}
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: key, Depth: depth,
			Expandable: n > 0, Expanded: exp,
			Style: st, Suffix: suf, SuffixStyle: sufSt,
			Data: &nodeMeta{segs: segs, slice: true},
		})
		if exp {
			for i := range n {
				a.emitElem(name, fv.Index(i), append(slices.Clone(segs), strconv.Itoa(i)), i, depth+1, unset)
			}
		}
	default:
		lst := st
		if !unset && omitEmpty && fv.IsZero() {
			lst.Fg = a.theme.HintFg // serialization would omit it: default
		}
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: key, Depth: depth,
			Style: lst, Suffix: "= " + formatLeaf(fv), SuffixStyle: sufSt,
			Data: &nodeMeta{segs: segs, leaf: true, ro: depth == 0 && key == "name"},
		})
	}
}

func (a *tuiApp) emitElem(name string, ev reflect.Value, segs []string, i, depth int, unset bool) {
	full := name + "." + strings.Join(segs, ".")
	st := tui.Style{Fg: a.theme.Fg}
	if unset {
		st.Fg = a.theme.HintFg
	}
	sufSt := tui.Style{Fg: a.theme.HintFg}
	label := "[" + strconv.Itoa(i) + "]"

	if ev.Kind() == reflect.Struct {
		exp := a.exp.IsExpanded(full)
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: label, Depth: depth,
			Expandable: true, Expanded: exp,
			Style: st, Suffix: summarize(ev), SuffixStyle: sufSt,
			Data: &nodeMeta{segs: segs, elem: true},
		})
		if exp {
			a.walkStruct(name, ev, segs, depth+1, unset)
		}
		return
	}
	a.nodes = append(a.nodes, tui.TreeNode{
		Key: full, Label: label, Depth: depth,
		Style: st, Suffix: "= " + formatLeaf(ev), SuffixStyle: sufSt,
		Data: &nodeMeta{segs: segs, leaf: true, elem: true},
	})
}

// summarize picks a distinguishing field for a composite's suffix, so a list
// of layers or procs is scannable without expanding each one. Generic on
// purpose: the preference list covers Source/Proc (kind), Layer/Bus (name),
// TrackDef (instr), StepDef (pos) without naming any of those types.
var summaryPref = []string{"name", "kind", "instr", "pos"}

func summarize(sv reflect.Value) string {
	idx := tagIndex(sv.Type())
	for _, k := range summaryPref {
		fi, ok := idx[k]
		if !ok {
			continue
		}
		f := sv.Field(fi)
		switch f.Kind() {
		case reflect.String:
			if s := f.String(); s != "" {
				return s
			}
		case reflect.Int:
			return strconv.FormatInt(f.Int(), 10) // pos 0 is the downbeat: always show
		}
	}
	return ""
}

// leafPrefill reads the current document value for the edit field. A path
// through an unset pointer chain has no value yet — return empty and let
// set's create-on-write resolve allocate it on commit.
func (a *tuiApp) leafPrefill(name string, segs []string) string {
	root, _, err := a.s.docRoot(name)
	if err != nil {
		return ""
	}
	v, err := resolve(reflect.ValueOf(root), segs, false)
	if err != nil {
		return ""
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.String {
		return v.String() // unquoted: formatLeaf's quotes would round-trip into the value
	}
	return formatLeaf(v)
}

// --- render ---

func (a *tuiApp) render() {
	w, h := a.w, a.h
	if w <= 0 || h <= 0 {
		return
	}
	if len(a.cells) != w*h {
		a.cells = make([]terminal.Cell, w*h)
	}
	a.sig = a.engineSig()
	a.draw(w, h)
	// Keys on a pane the frame left off go back to the view's own, drawn as such
	if main := a.mainPane(); a.focus != main && !slices.Contains(a.panes, a.focus) && (a.focus != focusInspector || a.view != viewBrowse) {
		a.focus = main
		a.draw(w, h)
	}
	a.term.Flush(a.cells, w, h)
}

// draw lays the frame out into the cells, noting the panes it shows
func (a *tuiApp) draw(w, h int) {
	for i := range a.cells {
		a.cells[i] = terminal.Cell{Rune: ' ', Fg: a.theme.Fg, Bg: a.theme.Bg}
	}
	root := tui.NewRegion(a.cells, w, 0, 0, w, h)
	a.panes = a.panes[:0]
	if a.tooSmall() {
		a.renderTooSmall(root)
		return
	}

	header, rest := tui.SplitVFixed(root, 1)
	body, footer := tui.SplitVFixed(rest, rest.H-1)
	a.renderHeader(header)
	if a.view == viewEdit {
		a.renderEditor(body)
	} else {
		a.renderBrowse(body)
	}
	a.renderFooter(footer)
	if a.view == viewBrowse && a.focus == focusInspector {
		a.renderInspector(root)
	}
	if a.files.open {
		a.renderFiles(root)
	}
	if a.help {
		a.renderHelp(root)
	}
	slices.Sort(a.panes)
	a.panes = slices.Compact(a.panes)
}

// renderTooSmall centers the shortfall, each line wrapped to the width there is,
// in place of every pane: nothing the keys would act on is drawn
func (a *tuiApp) renderTooSmall(r tui.Region) {
	type line struct {
		s  string
		fg color.RGB
	}
	var ls []line
	add := func(s string, fg color.RGB) {
		for _, w := range tui.WrapText(s, max(r.W-2, 1)) {
			ls = append(ls, line{w, fg})
		}
	}
	add(fmt.Sprintf("soundlab needs a terminal of at least %d×%d", minW, minH), a.theme.Warning)
	add(fmt.Sprintf("this one is %d×%d", a.w, a.h), a.theme.Fg)
	if a.quitArmed {
		add("unsaved changes: ^Q again discards them", a.theme.Warning)
	} else {
		add("enlarge it to go on, or ^Q to quit", a.theme.HintFg)
	}
	y := max((r.H-len(ls))/2, 0)
	for i, l := range ls {
		r.TextCenter(y+i, l.s, l.fg, a.theme.Bg, terminal.AttrNone)
	}
}

// renderHeader names the program and the view on the left and the engine's state
// on the right, dropping the least important sections first when narrow
func (a *tuiApp) renderHeader(r tui.Region) {
	r.Fill(a.theme.HeaderBg)
	title, fg := " soundlab", a.theme.HeaderFg
	if a.ed != nil {
		title += " › edit " + a.ed.name
		if a.fresh == a.ed.name {
			title, fg = " soundlab › new "+a.ed.name, a.theme.Warning
		}
	}
	r.Text(0, 0, title, fg, a.theme.HeaderBg, terminal.AttrBold)
	tw := tui.RuneLen(title) + 1

	lab := tui.Style{Fg: a.theme.HintFg}
	val := tui.Style{Fg: a.theme.HeaderFg}
	warn := tui.Style{Fg: a.theme.Warning}
	g := a.sig
	sym := "■"
	if g.running {
		sym = "▶"
	}
	secs := []tui.BarSection{
		{Value: sym + " " + transportPos(g.bar, g.step), ValueStyle: val, Priority: 5},
		{Label: "bpm ", Value: strconv.Itoa(a.s.bpm), LabelStyle: lab, ValueStyle: val, Priority: 4},
		{Value: a.s.keyName(), ValueStyle: val, Priority: 2},
	}
	if g.musicMuted {
		secs = append(secs, tui.BarSection{Value: "music muted", ValueStyle: warn, Priority: 5})
	}
	var names [audio.MusicSlots]string
	for i := range names {
		names[i] = patName(g.slots[i])
	}
	slots := len(secs)
	secs = append(secs, tui.BarSection{Label: "slots ", Value: strings.Join(names[:], "·"), LabelStyle: lab, ValueStyle: val, Priority: 1})
	if a.s.soundsModified() || a.s.pats.modified {
		secs = append(secs, tui.BarSection{Value: "● unsaved", ValueStyle: warn, Priority: 4})
	}
	if n := len(a.s.sounds.dirty) + len(a.s.voices.dirty) + len(a.s.pats.dirty); n > 0 {
		secs = append(secs, tui.BarSection{Label: "unapplied ", Value: strconv.Itoa(n), LabelStyle: lab, ValueStyle: warn, Priority: 3})
	}
	be := g.backend
	if g.silent || be == "" {
		be = "silent"
	}
	secs = append(secs, tui.BarSection{Value: be, ValueStyle: lab, Priority: 2})

	// Slot names shorten to what the row leaves them before any section drops
	right := r.Sub(tw, 0, r.W-tw, 1)
	used := 2
	for _, s := range secs {
		used += tui.RuneLen(s.Label) + tui.RuneLen(s.Value) + 3
	}
	// below six cells a name says nothing, and the bar drops the section instead
	if per := (tui.RuneLen(secs[slots].Value)-(used-right.W))/len(names) - 1; used > right.W && per >= 6 {
		for j := range names {
			names[j] = tui.Truncate(names[j], per)
		}
		secs[slots].Value = strings.Join(names[:], "·")
	}
	right.StatusBar(0, secs, tui.BarOpts{Bg: a.theme.HeaderBg, Align: tui.BarAlignRight,
		SepStyle: tui.Style{Fg: a.theme.Border}})
}

// transportPos is a playhead as bar.beat.sixteenth, counting from one
func transportPos(bar int64, step int) string {
	in := step % audio.StepsPerBar
	return fmt.Sprintf("%d.%d.%d", bar+1, in/audio.StepsPerBeat+1, in%audio.StepsPerBeat+1)
}

// renderBrowse lays out the browser beside the mixer over the log, the log taking
// a third of the body and never fewer than two lines
func (a *tuiApp) renderBrowse(r tui.Region) {
	logH := min(max(r.H/3, 4), 12)
	main, logR := tui.SplitVFixed(r, r.H-logH)
	brW := min(max(main.W*2/5, 30), 52)
	brR, mixR := tui.SplitHFixed(main, brW)
	a.renderBrowser(brR)
	a.renderMixer(mixR)
	a.renderLog(logR)
}

// paneBorder is the focus color on the pane the keys go to, and notes the pane as
// one Tab reaches
func (a *tuiApp) paneBorder(f focusArea) color.RGB {
	a.panes = append(a.panes, f)
	if a.focus == f {
		return a.theme.Selected
	}
	return a.theme.Border
}

// column is one fixed-width field of a browser row after the name
type column struct {
	head string
	w    int
}

func (a *tuiApp) renderBrowser(r tui.Region) {
	content := r.Pane(tui.PaneOpts{Border: tui.LineSingle, BorderFg: a.paneBorder(focusMain), Bg: a.theme.Bg})
	// The tabs are the title: patterns first, the active one lit
	x := 2
	for k, n := range [kindCount]int{len(a.s.pats.order), len(a.s.sounds.order), len(a.s.voices.order)} {
		tab := fmt.Sprintf(" %ss %d ", kindWord(k), n)
		fg, attr := a.theme.HintFg, terminal.AttrNone
		if k == a.kind {
			fg, attr = a.theme.HeaderFg, terminal.AttrBold
		}
		r.Text(x, 0, tab, fg, a.theme.Bg, attr)
		x += tui.RuneLen(tab) + 1
	}

	names := a.browserNames()
	if len(names) == 0 {
		content.TextBlock(0, 0, "empty — :builtin "+kindWord(a.kind)+" or :load", a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
		return
	}
	cols := []column{{"role", 6}, {"steps", 5}, {"trk", 3}}
	switch a.kind {
	case kindSound:
		cols = []column{{"role", 4}, {"layer", 5}, {"secs", 5}}
	case kindVoice:
		cols = []column{{"wave", 6}, {"level", 5}}
	}
	// Columns drop from the right until the name keeps a dozen cells
	avail := content.W - 2
	for len(cols) > 0 && avail-colsWidth(cols) < 12 {
		cols = cols[:len(cols)-1]
	}
	nameW := max(avail-colsWidth(cols), 1)
	content.Text(2, 0, rowText("name", nameW, cols, func(c column) string { return c.head }), a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
	list := content.Sub(0, 1, content.W, content.H-1)

	items := make([]tui.ListItem, 0, len(names))
	for _, n := range names {
		it := tui.ListItem{Icon: tui.IconBullet, IconFg: a.theme.Unselected, TextStyle: tui.Style{Fg: a.theme.Fg}}
		var field func(column) string
		switch a.kind {
		case kindVoice:
			d, _ := a.s.voices.get(n)
			field = func(c column) string {
				if c.head == "wave" {
					return cmp.Or(d.Wave, "sine")
				}
				return strconv.FormatFloat(cmp.Or(d.Level, 1), 'f', 2, 64)
			}
			if a.s.voices.isDirty(n) {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}
		case kindSound:
			d, _ := a.s.sounds.get(n)
			role := "sfx"
			if i, ok := audio.InstrumentByName(n); ok && i.IsDrum() {
				role = "drum"
			}
			field = func(c column) string {
				switch c.head {
				case "role":
					return role
				case "layer":
					return strconv.Itoa(len(d.Layer))
				}
				return strconv.FormatFloat(soundDur(d), 'f', 2, 64)
			}
			if a.s.sounds.isDirty(n) {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}
		default:
			d, _ := a.s.pats.get(n)
			field = func(c column) string {
				switch c.head {
				case "role":
					return cmp.Or(d.Role, "-")
				case "steps":
					return strconv.Itoa(d.Steps)
				}
				return strconv.Itoa(len(d.Track))
			}
			dirty := a.s.pats.isDirty(n)
			if dirty {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}
			if id := audio.PatternIDByName(n); id != audio.PatternSilence {
				if si := a.slotOf(id); si >= 0 {
					it.Icon, it.IconFg = rune('0'+si), a.theme.Selected
					if dirty {
						it.IconFg = a.theme.Warning
					}
				}
			}
		}
		it.Text = rowText(n, nameW, cols, field)
		items = append(items, it)
	}
	c := min(a.brCursor[a.kind], len(items)-1)
	a.brCursor[a.kind] = c
	a.brPageH = list.H
	a.brScroll[a.kind] = tui.AdjustScroll(c, a.brScroll[a.kind], list.H, len(items))
	cur := a.theme.FocusBg
	if a.focus == focusMain {
		cur = a.theme.CursorBg
	}
	list.List(items, c, a.brScroll[a.kind], tui.ListOpts{CursorBg: cur, DefaultBg: a.theme.Bg})
}

func colsWidth(cols []column) int {
	w := 0
	for _, c := range cols {
		w += c.w + 1
	}
	return w
}

// rowText lays a name and its columns out at fixed widths, the columns right-aligned
func rowText(name string, nameW int, cols []column, field func(column) string) string {
	var b strings.Builder
	b.WriteString(tui.PadRight(tui.Truncate(name, nameW), nameW))
	for _, c := range cols {
		b.WriteByte(' ')
		b.WriteString(tui.PadLeft(tui.Truncate(field(c), c.w), c.w))
	}
	return b.String()
}

// slotRoles name what each sequencer slot plays, as a pattern's role names it
var slotRoles = [audio.MusicSlots]string{"rhythm", "melody", "fill", "bass"}

// renderMixer shows what each slot sounds on one timeline: as many bars of the
// sequencer's four-bar cycle as fit, around the playhead. Its rows, each slot's
// title and tracks, part the slots by a blank row where they fit and scroll under
// the cursor where they do not.
func (a *tuiApp) renderMixer(r tui.Region) {
	border := a.paneBorder(focusMixer)
	content := r.Pane(tui.PaneOpts{
		Title: "mixer", Border: tui.LineSingle,
		BorderFg: border, TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
	if content.H <= 0 {
		return
	}
	rows := a.mixRows()
	cur, top := mixIndex(rows, a.mixCur), mixIndex(rows, a.mixTop)
	if a.focus == focusMixer {
		top = tui.AdjustScroll(cur, top, content.H, len(rows))
	}
	top = min(top, max(len(rows)-content.H, 0))
	a.mixTop, a.mixPageH = rows[top], content.H
	spare := 0
	if len(rows) <= content.H {
		spare = min((content.H-len(rows))/audio.MusicSlots, 1)
	} else {
		r.Sub(0, 1, r.W, r.H-2).ScrollBarStyled(r.W-1, top, content.H, len(rows),
			tui.ScrollBarOpts{ThumbFg: border, TrackFg: border, Track: '│'})
	}
	tl := fitTimeline(content.W - mixLabW)
	for i, y := top, 0; i < len(rows) && y < content.H; i, y = i+1, y+1 {
		if rows[i].trk < 0 && i > top {
			y += spare
		}
		a.drawMixRow(content.Sub(0, y, content.W, 1), rows[i], tl, i == cur)
	}
}

// mixRow is a row of the mixer: a slot's title, trk -1, or one of its tracks
type mixRow struct{ slot, trk int }

// mixRows are the mixer's rows for what the slots play
func (a *tuiApp) mixRows() []mixRow {
	rows := make([]mixRow, 0, 4*audio.MusicSlots)
	for slot, id := range a.sig.slots {
		rows = append(rows, mixRow{slot, -1})
		if p := audio.GetPattern(id); p != nil && p.Steps > 0 {
			for t := range p.Tracks {
				rows = append(rows, mixRow{slot, t})
			}
		}
	}
	return rows
}

// mixIndex is row r's place among rows, or the place of the last row before it
// when its slot no longer plays that track
func mixIndex(rows []mixRow, r mixRow) int {
	at := 0
	for i, x := range rows {
		if x.slot < r.slot || x.slot == r.slot && x.trk <= r.trk {
			at = i
		}
	}
	return at
}

// mixLabW is the mixer's instrument column
const mixLabW = 7

// timeline is a window of the sequencer's four-bar cycle: bars of cw cells a step
type timeline struct{ bars, cw int }

// fitTimeline shows the most bars that fit w, then widens their steps
func fitTimeline(w int) timeline {
	width := func(t timeline) int {
		n := t.bars * audio.StepsPerBar
		return n*t.cw + n/audio.StepsPerBeat - 1
	}
	t := timeline{bars: 1, cw: 1}
	for _, b := range []int{4, 2} {
		if width(timeline{b, 1}) <= w {
			t.bars = b
			break
		}
	}
	for t.cw < 3 && width(timeline{t.bars, t.cw + 1}) <= w {
		t.cw++
	}
	return t
}

// drawMixRow draws a row of the mixer: a slot's number and role with what it plays
// under which group, motif and tiers, or one of its tracks along the timeline, the
// pattern repeating as it plays; the cursor's name is lit
func (a *tuiApp) drawMixRow(r tui.Region, row mixRow, tl timeline, cursor bool) {
	g, th := a.sig, &a.theme
	labBg := th.Bg
	if cursor {
		labBg = th.FocusBg
		if a.focus == focusMixer {
			labBg = th.CursorBg
		}
	}
	pat := audio.GetPattern(g.slots[row.slot])
	if row.trk < 0 {
		title := fmt.Sprintf("%d %s", row.slot, slotRoles[row.slot])
		r.Text(0, 0, title, th.HeaderFg, labBg, terminal.AttrBold)
		tx := tui.RuneLen(title) + 1
		if g.muted[row.slot] {
			r.Text(tx, 0, "muted", th.Warning, th.Bg, terminal.AttrBold)
			tx += len("muted ")
		}
		if pat == nil || pat.Steps <= 0 {
			r.Text(tx, 0, "-", th.Unselected, th.Bg, terminal.AttrNone)
			return
		}
		var tiers []string
		for t := range audio.IntensityCount {
			if pat.Tiers&(1<<t) != 0 {
				tiers = append(tiers, audio.Intensity(t).String())
			}
		}
		info := pat.Name
		for _, s := range []string{strings.Join(pat.Groups, "+"), pat.Motif, strings.Join(tiers, " ")} {
			if s != "" {
				info += " · " + s
			}
		}
		if bars := pat.Steps / audio.StepsPerBar; bars > 1 {
			info += fmt.Sprintf(" · bar %d/%d", g.step%pat.Steps/audio.StepsPerBar+1, bars)
		}
		r.Text(tx, 0, tui.Truncate(info, r.W-tx), th.HintFg, th.Bg, terminal.AttrNone)
		return
	}
	if pat == nil || pat.Steps <= 0 || row.trk >= len(pat.Tracks) {
		return
	}
	tr := &pat.Tracks[row.trk]
	r.Text(0, 0, tui.PadRight(tui.Truncate(audio.InstrumentName(tr.Instr), mixLabW-1), mixLabW-1), th.HintFg, labBg, terminal.AttrNone)
	span := tl.bars * audio.StepsPerBar
	first := g.step - g.step%span // the window holding the playhead
	for i := range span {
		c := first + i
		pos := c % pat.Steps
		var vel float64
		for _, ev := range tr.Events {
			if ev.Pos == pos {
				vel = max(vel, ev.Vel)
			}
		}
		ch, fg := velGlyph(vel), th.Fg
		switch {
		case vel == 0:
			ch, fg = '·', th.Unselected
		case g.muted[row.slot]:
			fg = th.Unselected
		}
		bg := th.Bg
		if g.running && c == g.step {
			bg = th.CursorBg
		}
		x := mixLabW + i*tl.cw + i/audio.StepsPerBeat
		if i > 0 && i%audio.StepsPerBar == 0 {
			r.Cell(x-1, 0, '│', th.Border, th.Bg, terminal.AttrNone)
		}
		for k := range tl.cw {
			c := ch
			if k > 0 && (vel == 0 || k == tl.cw-1) {
				c = ' ' // a hit fills its step but its last cell; a rest shows one dot
			}
			r.Cell(x+k, 0, c, fg, bg, terminal.AttrNone)
		}
	}
}

// velGlyph draws a velocity as a block's height
func velGlyph(v float64) rune { return meterGlyph(v, 1, 0) }

// logLines wraps the log to width w, oldest first
func (a *tuiApp) logLines(w int) []string {
	var out []string
	for _, l := range a.log.lines {
		out = append(out, tui.WrapText(l, max(w, 1))...)
	}
	return out
}

func (a *tuiApp) renderLog(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title: "log", Border: tui.LineSingle,
		BorderFg: a.paneBorder(focusLog), TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
	lines := a.logLines(content.W)
	a.logScroll = min(a.logScroll, max(len(lines)-content.H, 0))
	start := max(len(lines)-content.H-a.logScroll, 0)
	for y := 0; y < content.H && start+y < len(lines); y++ {
		l := lines[start+y]
		content.Text(0, y, l, a.logFg(l), a.theme.Bg, terminal.AttrNone)
	}
}

// renderMessage is the log's last line, for a view that shows no log
func (a *tuiApp) renderMessage(r tui.Region) {
	if n := len(a.log.lines); n > 0 {
		l := a.log.lines[n-1]
		r.Text(1, 0, tui.Truncate(l, r.W-2), a.logFg(l), a.theme.Bg, terminal.AttrNone)
	}
}

func (a *tuiApp) logFg(l string) color.RGB {
	switch {
	case strings.HasPrefix(l, "> "):
		return a.theme.HintFg
	case strings.HasPrefix(l, "error:"):
		return a.theme.Error
	}
	return a.theme.Fg
}

// stackBelow gives an editor view the rows it wants and the rest of the body to the
// mixer, over the log's last line; a screen too short for the mixer keeps the line
// alone. It returns the view's.
func (a *tuiApp) stackBelow(r tui.Region, want int) tui.Region {
	body, msg := tui.SplitVFixed(r, r.H-1)
	a.renderMessage(msg)
	if body.H-want < 3 { // the mixer's frame and a row
		return body
	}
	top, mix := tui.SplitVFixed(body, want)
	a.renderMixer(mix)
	return top
}

func (a *tuiApp) renderFooter(r tui.Region) {
	switch a.mode {
	case modeCommand:
		r.TextField(a.cmdField, tui.TextFieldOpts{Prefix: ":", Focused: true, Border: tui.LineNone})
		return
	case modeEdit:
		pre := tui.Truncate(a.editPath, r.W/2) + " = "
		r.TextField(a.editField, tui.TextFieldOpts{Prefix: pre, Focused: true, Border: tui.LineNone})
		return
	case modePrompt:
		r.TextField(a.editField, tui.TextFieldOpts{Prefix: a.promptLabel, Focused: true, Border: tui.LineNone})
		return
	}
	r.Fill(a.theme.HeaderBg)
	r.Text(1, 0, tui.Truncate(a.footHint(), r.W-2), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
}

// footHint names the keys that matter where the keyboard is; ? has the rest
func (a *tuiApp) footHint() string {
	switch {
	case a.files.open:
		return "j/k move  enter open/load/rip  m merge  s save here  h up  esc close  ? keys"
	case a.focus == focusMixer:
		return "j/k track  space mute  e edit  n new  PgUp/PgDn page  g/G ends  tab next pane  esc back  ? keys"
	case a.focus == focusKeys:
		return a.keysHint()
	case a.focus == focusGrid:
		return a.gridHint()
	case a.focus == focusInspector:
		return "j/k move  h/l fold  enter edit  a add  x del  space play  esc close  ? keys"
	case a.focus == focusLog:
		return "j/k scroll  g/G ends  tab next pane  esc back  : cmd  ? keys  ^Q quit"
	case a.kind == kindPattern:
		return "j/k move  space play  0-3 slot  n new  e edit  enter inspect  h/l tabs  tab next pane  ? keys  ^Q quit"
	}
	return "j/k move  space play  n new  e edit  enter inspect  h/l tabs  / find  tab next pane  ? keys  ^Q quit"
}

// renderHelp lays the view's key reference, then the global keys, over
// everything: keys in a column, what they do wrapped beside them
func (a *tuiApp) renderHelp(root tui.Region) {
	th := &a.theme
	secs := append(a.helpFor(), helpMixer, helpGlobal)
	keyW := 0
	for _, sec := range secs {
		for _, l := range sec.lines {
			keyW = max(keyW, tui.RuneLen(l.keys))
		}
	}
	opts := tui.DefaultOverlayOpts("keys — j/k scroll, any other key closes")
	opts.Bg, opts.Fg, opts.TitleBg, opts.TitleFg = th.Bg, th.Selected, th.HeaderBg, th.HeaderFg
	opts.Width = min(root.W-2, 96)
	whatW := max(opts.Width-4-keyW-3, 10)
	type row struct {
		keys, what string
		title      bool
	}
	var rows []row
	for _, sec := range secs {
		rows = append(rows, row{keys: sec.title, title: true})
		for _, l := range sec.lines {
			for i, w := range tui.WrapText(l.what, whatW) {
				k := ""
				if i == 0 {
					k = l.keys
				}
				rows = append(rows, row{keys: k, what: w})
			}
		}
	}
	opts.Height = min(len(rows)+2, root.H-2)
	content := root.Overlay(opts).Content
	a.helpTop = min(a.helpTop, max(len(rows)-content.H, 0))
	for y := 0; y < content.H && a.helpTop+y < len(rows); y++ {
		r := rows[a.helpTop+y]
		if r.title {
			content.Text(1, y, r.keys, th.HeaderFg, th.Bg, terminal.AttrBold)
			continue
		}
		content.Text(2, y, r.keys, th.Selected, th.Bg, terminal.AttrNone)
		content.Text(3+keyW, y, r.what, th.Fg, th.Bg, terminal.AttrNone)
	}
}

// renderInspector lays the selection's tree over the panes until Esc
func (a *tuiApp) renderInspector(root tui.Region) {
	name := a.selName()
	title := "inspector"
	if name != "" {
		state := "edit"
		if a.fresh == name {
			state = "new"
		}
		title = state + " " + kindWord(a.kind) + " " + name
	}
	opts := tui.DefaultOverlayOpts(title + " — esc closes")
	opts.Bg, opts.Fg, opts.TitleBg, opts.TitleFg = a.theme.Bg, a.theme.Selected, a.theme.HeaderBg, a.theme.HeaderFg
	opts.Width, opts.Height = min(root.W*8/10, 120), min(root.H*8/10, 48)
	content := root.Overlay(opts).Content
	if name == "" {
		content.Text(0, 0, "select an entry in the browser (Enter)", a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
		a.nodes = a.nodes[:0]
		return
	}
	// Rebuild every frame: specs are bounded small (MaxSoundLayers etc.) and
	// a fresh walk is what keeps the tree honest after any Execute mutation.
	if key := fmt.Sprintf("%d/%s", a.kind, name); key != a.selKey {
		a.selKey = key
		a.tree.JumpStart()
	}
	a.buildNodes(name)
	a.tree.SetVisible(content.H)
	if a.tree.Cursor >= len(a.nodes) {
		a.tree.JumpEnd(len(a.nodes))
	}
	a.tree.AdjustScroll(len(a.nodes))
	content.Tree(a.nodes, a.tree.Cursor, a.tree.Scroll, tui.TreeOpts{
		CursorBg: a.theme.CursorBg, DefaultBg: a.theme.Bg, LineMode: tui.TreeLinesNone,
	})
}
