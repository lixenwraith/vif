package main

// Data and render side of the TUI: log capture, browser lists, the
// reflection-built inspector tree, header/footer. The inspector walks the
// same toml key space path.go addresses, so what it shows is exactly what
// set/add/del/unset can reach.

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/audio"
)

// --- log capture ---

// logBuffer captures Session output for the log pane. Main-goroutine
// confined: every Execute call and every render runs on the event loop, so
// no lock. A partial line (no trailing newline) is held until completed.
type logBuffer struct {
	lines []string
	part  string
	max   int
}

func newLogBuffer(max int) *logBuffer { return &logBuffer{max: max} }

func (l *logBuffer) Write(p []byte) (int, error) {
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

func (l *logBuffer) push(s string) {
	l.lines = append(l.lines, s)
	if n := len(l.lines) - l.max; n > 0 {
		l.lines = slices.Delete(l.lines, 0, n)
	}
}

// --- selection ---

func (a *tuiApp) browserNames() []string {
	if a.kind == kindSound {
		return a.s.sounds.order
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

// emitField renders one field. A nil pointer sub-table (Burst, Vibrato) is
// walked through its zero value so fields that do not exist yet can be
// browsed and edited — set's create-on-write resolve allocates on commit;
// the whole substituted subtree renders dimmed. A leaf also dims when its
// tag is omitempty and the value is zero: exactly the fields a save would
// omit, i.e. the ones resolving to package defaults.
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
	sfx := tui.Style{Fg: a.theme.HintFg}

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
			Style: st, Suffix: suf, SuffixStyle: sfx,
			Data: &nodeMeta{segs: segs},
		})
		if exp {
			a.walkStruct(name, fv, segs, depth+1, unset)
		}
	case reflect.Slice:
		n := fv.Len()
		exp := a.exp.IsExpanded(full) && n > 0
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: key, Depth: depth,
			Expandable: n > 0, Expanded: exp,
			// "(n)" not "[n]": bus[1] read as "routed to bus 1" when it
			// meant "one bus defined".
			Style: st, Suffix: fmt.Sprintf("(%d)", n), SuffixStyle: sfx,
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
			Style: lst, Suffix: "= " + formatLeaf(fv), SuffixStyle: sfx,
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
	sfx := tui.Style{Fg: a.theme.HintFg}
	label := "[" + strconv.Itoa(i) + "]"

	if ev.Kind() == reflect.Struct {
		exp := a.exp.IsExpanded(full)
		a.nodes = append(a.nodes, tui.TreeNode{
			Key: full, Label: label, Depth: depth,
			Expandable: true, Expanded: exp,
			Style: st, Suffix: summarize(ev), SuffixStyle: sfx,
			Data: &nodeMeta{segs: segs, elem: true},
		})
		if exp {
			a.walkStruct(name, ev, segs, depth+1, unset)
		}
		return
	}
	a.nodes = append(a.nodes, tui.TreeNode{
		Key: full, Label: label, Depth: depth,
		Style: st, Suffix: "= " + formatLeaf(ev), SuffixStyle: sfx,
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
	if w < 4 || h < 4 {
		return
	}
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: a.theme.Fg, Bg: a.theme.Bg}
	}
	root := tui.NewRegion(cells, w, 0, 0, w, h)
	if w < 60 || h < 16 {
		root.Text(0, 0, "soundlab: terminal too small (need 60x16)", a.theme.Warning, a.theme.Bg, terminal.AttrNone)
		a.term.Flush(cells, w, h)
		return
	}

	// The strip between the log and the input line belongs to at most one
	// mode at a time: piano rows or the beat grid. Its height is decided
	// here, before the vertical split, so the panes above shrink to fit.
	stripH := 0
	switch a.mode {
	case modePiano:
		stripH = pianoRowsH
	case modeBeat:
		if d := a.beatPat(); d != nil {
			stripH = min(max(len(d.Track), 2), 6) // grows with tracks, capped
		}
	}

	header, rest := tui.SplitVFixed(root, 1)
	body, bottom := tui.SplitVFixed(rest, rest.H-1-stripH)
	var strip, input tui.Region
	if stripH > 0 {
		strip, input = tui.SplitVFixed(bottom, stripH)
	} else {
		input = bottom
	}
	main, logR := tui.SplitVFixed(body, body.H-logPaneH)
	brR, insR := tui.SplitHFixed(main, main.W*32/100)

	a.renderHeader(header)
	a.renderBrowser(brR)
	a.renderInspector(insR)
	a.renderLog(logR)
	switch a.mode {
	case modeBeat:
		a.renderBeat(strip)
	case modePiano:
		a.renderPiano(strip)
	}
	a.renderInput(input)

	a.term.Flush(cells, w, h)
}

func (a *tuiApp) borderFg(f focusArea) color.RGB {
	if a.focus == f {
		return a.theme.Selected
	}
	return a.theme.Border
}

func (a *tuiApp) renderHeader(r tui.Region) {
	r.Fill(a.theme.HeaderBg)
	r.Text(1, 0, "soundlab", a.theme.HeaderFg, a.theme.HeaderBg, terminal.AttrBold)

	bar, step, running := a.s.eng.Transport()
	sym := "■"
	if running {
		sym = "▶"
	}
	be := a.s.eng.BackendName()
	if a.s.eng.IsSilent() || be == "" {
		be = "silent"
	}
	lab := tui.Style{Fg: a.theme.HintFg}
	val := tui.Style{Fg: a.theme.HeaderFg}

	rest := []tui.BarSection{
		{Label: "bpm ", Value: strconv.Itoa(a.s.bpm), LabelStyle: lab, ValueStyle: val, Priority: 2},
		{Label: "dirty ", Value: strconv.Itoa(len(a.s.sounds.dirty) + len(a.s.pats.dirty)), LabelStyle: lab, ValueStyle: val, Priority: 2},
		{Value: be, ValueStyle: val, Priority: 1},
	}
	if a.s.sounds.modified || a.s.pats.modified {
		rest = append([]tui.BarSection{
			{Value: "● unsaved", ValueStyle: tui.Style{Fg: a.theme.Warning}, Priority: 2},
		}, rest...)
	}
	trans := tui.BarSection{Value: fmt.Sprintf("%s %d:%02d", sym, bar, step), ValueStyle: val, Priority: 3}

	// Slot names shorten only when the full form does not fit (was an
	// unconditional 12-char cap): measure the fixed sections, give the
	// slots whatever width remains.
	names := [3]string{
		patName(a.s.eng.SlotPattern(0)),
		patName(a.s.eng.SlotPattern(1)),
		patName(a.s.eng.SlotPattern(2)),
	}
	fixed := 1 + tui.RuneLen("soundlab") + 2 + tui.RuneLen(trans.Value) + 3
	for _, s := range rest {
		fixed += tui.RuneLen(s.Label) + tui.RuneLen(s.Value) + 3
	}
	budget := r.W - fixed - tui.RuneLen("slots ") - 2 // two '·' separators
	if full := tui.RuneLen(names[0]) + tui.RuneLen(names[1]) + tui.RuneLen(names[2]); full > budget {
		per := max(budget/3, 6)
		for i := range names {
			names[i] = tui.Truncate(names[i], per)
		}
	}
	secs := append([]tui.BarSection{
		trans,
		{Label: "slots ", Value: names[0] + "·" + names[1] + "·" + names[2], LabelStyle: lab, ValueStyle: val, Priority: 1},
	}, rest...)
	r.StatusBar(0, secs, tui.BarOpts{Bg: a.theme.HeaderBg, Align: tui.BarAlignRight})
}

func (a *tuiApp) renderBrowser(r tui.Region) {
	ns, np := len(a.s.sounds.order), len(a.s.pats.order)
	title := fmt.Sprintf("[sounds %d]  patterns %d", ns, np)
	if a.kind == kindPattern {
		title = fmt.Sprintf("sounds %d  [patterns %d]", ns, np)
	}
	content := r.Pane(tui.PaneOpts{
		Title: title, Border: tui.LineSingle,
		BorderFg: a.borderFg(focusBrowser), TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
	names := a.browserNames()
	if len(names) == 0 {
		content.Text(0, 0, "empty — :builtin sound|pattern or :load", a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
		return
	}
	items := make([]tui.ListItem, 0, len(names))
	for _, n := range names {
		var it tui.ListItem
		if a.kind == kindSound {
			d, _ := a.s.sounds.get(n)
			it.Text = fmt.Sprintf("%-20s %2dL %4.1fs", tui.Truncate(n, 20), len(d.Layer), soundDur(d))
			it.Icon, it.IconFg = tui.IconBullet, a.theme.Unselected
			if a.s.sounds.isDirty(n) {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}
		} else {
			d, _ := a.s.pats.get(n)
			it.Text = fmt.Sprintf("%-20s %2ds %2dT", tui.Truncate(n, 20), d.Steps, len(d.Track))
			it.Icon, it.IconFg = tui.IconBullet, a.theme.Unselected

			isDirty := a.s.pats.isDirty(n)
			if isDirty {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}

			if id := audio.PatternIDByName(n); id != audio.PatternSilence {
				for si := range 3 {
					if a.s.eng.SlotPattern(si) == id {
						it.Icon = rune('0' + si)
						if isDirty {
							it.IconFg = a.theme.Warning
						} else {
							it.IconFg = a.theme.Selected
						}
						break
					}
				}
			}
		}
		it.TextStyle = tui.Style{Fg: a.theme.Fg}
		items = append(items, it)
	}
	c := a.brCursor[a.kind]
	if c > len(items)-1 {
		c = len(items) - 1
		a.brCursor[a.kind] = c
	}
	a.brScroll[a.kind] = tui.AdjustScroll(c, a.brScroll[a.kind], content.H, len(items))
	cur := a.theme.FocusBg
	if a.focus == focusBrowser {
		cur = a.theme.CursorBg
	}
	content.List(items, c, a.brScroll[a.kind], tui.ListOpts{CursorBg: cur, DefaultBg: a.theme.Bg})
}

func (a *tuiApp) renderInspector(r tui.Region) {
	name := a.selName()
	title := name
	if title == "" {
		title = "inspector"
	}
	content := r.Pane(tui.PaneOpts{
		Title: title, Border: tui.LineSingle,
		BorderFg: a.borderFg(focusInspector), TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
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
	cur := a.theme.FocusBg
	if a.focus == focusInspector {
		cur = a.theme.CursorBg
	}
	content.Tree(a.nodes, a.tree.Cursor, a.tree.Scroll, tui.TreeOpts{
		CursorBg: cur, DefaultBg: a.theme.Bg, LineMode: tui.TreeLinesNone,
	})
}

func (a *tuiApp) renderLog(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title: "log", Border: tui.LineSingle,
		BorderFg: a.borderFg(focusLog), TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
	lines := a.log.lines
	if maxUp := len(lines) - content.H; a.logScroll > maxUp {
		if maxUp < 0 {
			maxUp = 0
		}
		a.logScroll = maxUp
	}
	start := max(len(lines)-content.H-a.logScroll, 0)
	for y := range content.H {
		li := start + y
		if li >= len(lines) {
			break
		}
		fg := a.theme.Fg
		if strings.HasPrefix(lines[li], "> ") {
			fg = a.theme.HintFg
		} else if strings.HasPrefix(lines[li], "error:") {
			fg = a.theme.Error
		}
		content.Text(0, y, lines[li], fg, a.theme.Bg, terminal.AttrNone)
	}
}

func (a *tuiApp) renderInput(r tui.Region) {
	switch a.mode {
	case modeCommand:
		r.TextField(a.cmdField, tui.TextFieldOpts{Prefix: ":", Focused: true, Border: tui.LineNone})
	case modeEdit:
		pre := tui.Truncate(a.editPath, r.W/2) + " = "
		r.TextField(a.editField, tui.TextFieldOpts{Prefix: pre, Focused: true, Border: tui.LineNone})
	case modePiano:
		r.Fill(a.theme.HeaderBg)
		recBadge := ""
		if a.rec {
			recBadge = "REC ● "
		}
		msg := fmt.Sprintf("PIANO %s%s  oct %d  vel %.2f   z/q play  [ ] oct  ↑↓ vel  tab instr  ^S rec  esc exit",
			recBadge, a.pianoInstr, a.pianoOct, a.pianoVel)
		r.Text(1, 0, tui.Truncate(msg, r.W-2), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	case modeBeat:
		r.Fill(a.theme.HeaderBg)
		msg := fmt.Sprintf("BEAT %s  hjkl move  space toggle  +/- 1-9 0 vel  t instr  a/x track  enter apply+play  esc close",
			a.beat.name)
		r.Text(1, 0, tui.Truncate(msg, r.W-2), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	case modePrompt:
		r.TextField(a.editField, tui.TextFieldOpts{Prefix: a.promptLabel, Focused: true, Border: tui.LineNone})
	default:
		r.Fill(a.theme.HeaderBg)
		r.Text(1, 0, a.footHint(), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	}
}

// footHint forks on focus first — that is the axis that changes which keys
// are live. The browser hint additionally forks on the active tab, because
// space/0-2/b mean different things per kind; inspector and log hints are
// tab-independent.
func (a *tuiApp) footHint() string {
	switch a.focus {
	case focusInspector:
		return "j/k move  h/l fold  H/L all  enter edit  space play  a add  x del  esc back  dim=default  p piano  m reset  ^S save  : cmd  ^Q quit"
	case focusLog:
		return "j/k scroll  g/G ends  tab/shift-tab focus  p piano  m/M music  ^S save  : cmd  ^Q quit"
	default: // focusBrowser
		if a.kind == kindPattern {
			return "j/k move  h/l kind  space play  0-2 slot  b beat  n/c/w new/clone/write  + mix  d del  a/v/r apply/val/rev  p piano  m reset  ^S save  : cmd  ^Q quit"
		}
		return "j/k move  h/l kind  space play  enter inspect  n/c/w new/clone/write  + mix  d del  a/v/r apply/val/rev  p piano  m reset  ^S save  : cmd  ^Q quit"
	}
}
