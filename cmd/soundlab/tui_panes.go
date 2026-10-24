//go:build !vif_headless && !vif_noaudio && !wasm

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
	"github.com/lixenwraith/vif/pkg/audio"
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

// emitField renders one field. A nil pointer sub-table (Burst, Vibrato) is walked
// through its zero value so fields that do not exist yet can be browsed and edited
// (set's create-on-write resolve allocates on commit); the substituted subtree
// renders dimmed. A leaf also dims when its tag is omitempty and its value zero:
// the fields a save would omit, which resolve to package defaults.
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
	// mode at a time: the keys or the beat grid, each under a family row and in
	// a frame. Its height is decided here, before the vertical split, so the
	// panes above shrink to fit.
	stripH := 0
	switch a.mode {
	case modeKeys:
		stripH = keysRowsH + 3
	case modeBeat:
		if d := a.beatPat(); d != nil {
			stripH = min(max(len(d.Track), 2), 6) + 3 // grows with tracks, capped
		}
	}

	header, rest := tui.SplitVFixed(root, 1)
	stripH = min(stripH, max(rest.H-1-logPaneH-3, 0)) // a short terminal keeps its log
	body, bottom := tui.SplitVFixed(rest, rest.H-1-stripH)
	var strip, input tui.Region
	if stripH > 0 {
		strip, input = tui.SplitVFixed(bottom, stripH)
	} else {
		input = bottom
	}
	main, logR := tui.SplitVFixed(body, body.H-logPaneH)
	brR, mixR := tui.SplitHFixed(main, main.W*36/100)

	a.renderHeader(header)
	a.renderBrowser(brR)
	a.renderMixer(mixR)
	a.renderLog(logR)
	switch a.mode {
	case modeBeat:
		a.renderBeat(a.stripPane(strip, "beat · "+a.beat.name))
	case modeKeys:
		a.renderKeys(a.stripPane(strip, "keys"))
	}
	a.renderInput(input)
	if a.focus == focusInspector {
		a.renderInspector(root)
	}
	if a.help {
		a.renderHelp(root)
	}

	a.term.Flush(cells, w, h)
}

// borderFg is the focus color on the focused pane, unless a strip has the keys
func (a *tuiApp) borderFg(f focusArea) color.RGB {
	if a.focus == f && !a.stripActive() {
		return a.theme.Selected
	}
	return a.theme.Border
}

// stripActive reports whether the keys or the beat editor has the keyboard
func (a *tuiApp) stripActive() bool {
	return a.mode == modeKeys || a.mode == modeBeat
}

// stripPane frames the strip in the focus color, which the panes above give up
func (a *tuiApp) stripPane(r tui.Region, title string) tui.Region {
	return r.Pane(tui.PaneOpts{
		Title: title, Border: tui.LineSingle,
		BorderFg: a.theme.Selected, TitleFg: a.theme.Selected, Bg: a.theme.Bg,
	})
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

	// Slot names shorten only when the full form does not fit: measure the
	// fixed sections, give the slots whatever width remains.
	var names [audio.MusicSlots]string
	full := 0
	for i := range names {
		names[i] = patName(a.s.eng.SlotPattern(i))
		full += tui.RuneLen(names[i])
	}
	fixed := 1 + tui.RuneLen("soundlab") + 2 + tui.RuneLen(trans.Value) + 3
	for _, s := range rest {
		fixed += tui.RuneLen(s.Label) + tui.RuneLen(s.Value) + 3
	}
	budget := r.W - fixed - tui.RuneLen("slots ") - (len(names) - 1) // '·' separators
	if full > budget {
		per := max(budget/len(names), 6)
		for i := range names {
			names[i] = tui.Truncate(names[i], per)
		}
	}
	secs := append([]tui.BarSection{
		trans,
		{Label: "slots ", Value: strings.Join(names[:], "·"), LabelStyle: lab, ValueStyle: val, Priority: 1},
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
	// A header row names the columns; the name takes what the pane has left
	nameW := max(content.W-16, 12)
	head := fmt.Sprintf("  %-*s %5s %6s", nameW, "name", "steps", "tracks")
	if a.kind == kindSound {
		head = fmt.Sprintf("  %-*s %5s %6s", nameW, "name", "layer", "secs")
	}
	content.Text(0, 0, head, a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
	content = content.Sub(0, 1, content.W, content.H-1)
	items := make([]tui.ListItem, 0, len(names))
	for _, n := range names {
		var it tui.ListItem
		if a.kind == kindSound {
			d, _ := a.s.sounds.get(n)
			it.Text = fmt.Sprintf("%-*s %5d %6.2f", nameW, tui.Truncate(n, nameW), len(d.Layer), soundDur(d))
			it.Icon, it.IconFg = tui.IconBullet, a.theme.Unselected
			if a.s.sounds.isDirty(n) {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}
		} else {
			d, _ := a.s.pats.get(n)
			it.Text = fmt.Sprintf("%-*s %5d %6d", nameW, tui.Truncate(n, nameW), d.Steps, len(d.Track))
			it.Icon, it.IconFg = tui.IconBullet, a.theme.Unselected

			isDirty := a.s.pats.isDirty(n)
			if isDirty {
				it.Icon, it.IconFg = '*', a.theme.Warning
			}

			if id := audio.PatternIDByName(n); id != audio.PatternSilence {
				for si := range audio.MusicSlots {
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
	a.brPageH = content.H
	a.brScroll[a.kind] = tui.AdjustScroll(c, a.brScroll[a.kind], content.H, len(items))
	cur := a.theme.FocusBg
	if a.focus == focusBrowser && !a.stripActive() {
		cur = a.theme.CursorBg
	}
	content.List(items, c, a.brScroll[a.kind], tui.ListOpts{CursorBg: cur, DefaultBg: a.theme.Bg})
}

// slotRoles name what each sequencer slot plays
var slotRoles = [audio.MusicSlots]string{"rhythm", "melody", "fill", "bass"}

// renderMixer shows what each slot sounds, as a mixer would: the pattern's grid for
// the bar at the playhead under the group, motif and tiers it was drawn with.
// Enter on the browser opens the full inspector over it.
func (a *tuiApp) renderMixer(r tui.Region) {
	content := r.Pane(tui.PaneOpts{
		Title: "mixer", Border: tui.LineSingle,
		BorderFg: a.theme.Border, TitleFg: a.theme.HeaderFg, Bg: a.theme.Bg,
	})
	_, step, running := a.s.eng.Transport()
	secH := content.H / audio.MusicSlots
	if secH < 2 {
		return
	}
	for slot := range audio.MusicSlots {
		a.renderSlot(content.Sub(0, slot*secH, content.W, secH), slot, step, running)
	}
}

func (a *tuiApp) renderSlot(r tui.Region, slot, step int, running bool) {
	pat := audio.GetPattern(a.s.eng.SlotPattern(slot))
	r.Divider(0, "", tui.LineSingle, a.theme.Border)
	title := fmt.Sprintf(" %d %s ", slot, slotRoles[slot])
	if pat == nil {
		r.Text(1, 0, title+" - ", a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
		return
	}
	var tiers []string
	for t := range audio.IntensityCount {
		if pat.Tiers&(1<<t) != 0 {
			tiers = append(tiers, audio.Intensity(t).String())
		}
	}
	local := step % pat.Steps
	win := local - local%audio.StepsPerBar
	info := fmt.Sprintf(" %s · %s · %s · %s ", pat.Name, strings.Join(pat.Groups, "+"), pat.Motif, strings.Join(tiers, " "))
	if bars := pat.Steps / audio.StepsPerBar; bars > 1 {
		info += fmt.Sprintf("bar %d/%d ", win/audio.StepsPerBar+1, bars)
	}
	r.Text(1, 0, title, a.theme.HeaderFg, a.theme.Bg, terminal.AttrBold)
	r.Text(1+tui.RuneLen(title), 0, tui.Truncate(info, r.W-2-tui.RuneLen(title)), a.theme.HintFg, a.theme.Bg, terminal.AttrNone)

	rows := r.H - 1
	for ti := range pat.Tracks {
		if ti >= rows {
			break
		}
		y := 1 + ti
		if ti == rows-1 && len(pat.Tracks) > rows {
			r.Text(1, y, fmt.Sprintf("+%d more", len(pat.Tracks)-rows+1), a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
			break
		}
		tr := &pat.Tracks[ti]
		r.Text(1, y, fmt.Sprintf("%-6s", tr.Instr), a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
		for st := range audio.StepsPerBar {
			var vel float64
			for _, ev := range tr.Events {
				if ev.Pos == win+st {
					vel = max(vel, ev.Vel)
				}
			}
			ch := '·'
			switch {
			case vel >= 0.75:
				ch = '█'
			case vel >= 0.5:
				ch = '▆'
			case vel >= 0.25:
				ch = '▃'
			case vel > 0:
				ch = '▁'
			}
			bg := a.theme.Bg
			if running && win+st == local {
				bg = a.theme.CursorBg
			}
			r.Cell(8+st+st/4, y, ch, a.theme.Fg, bg, terminal.AttrNone)
		}
	}
}

// renderHelp lays the key reference over everything; any key takes it down
func (a *tuiApp) renderHelp(root tui.Region) {
	opts := tui.DefaultOverlayOpts("keys — any key closes")
	opts.Bg, opts.Fg, opts.TitleBg, opts.TitleFg = a.theme.Bg, a.theme.Selected, a.theme.HeaderBg, a.theme.HeaderFg
	w := 0
	for _, l := range keyHelp {
		w = max(w, tui.RuneLen(l))
	}
	opts.Width, opts.Height = min(w+4, root.W-2), min(len(keyHelp)+2, root.H-2)
	content := root.Overlay(opts).Content
	for y, l := range keyHelp {
		if y >= content.H {
			break
		}
		content.Text(1, y, tui.Truncate(l, content.W-2), a.theme.Fg, a.theme.Bg, terminal.AttrNone)
	}
}

// renderInspector lays the selection's tree over the panes until Esc
func (a *tuiApp) renderInspector(root tui.Region) {
	name := a.selName()
	title := name
	if title == "" {
		title = "inspector"
	}
	opts := tui.DefaultOverlayOpts(title + " — esc closes")
	opts.Bg, opts.Fg, opts.TitleBg, opts.TitleFg = a.theme.Bg, a.theme.Selected, a.theme.HeaderBg, a.theme.HeaderFg
	opts.Width, opts.Height = root.W*8/10, root.H*8/10
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
	case modeKeys:
		r.Fill(a.theme.HeaderBg)
		recBadge := ""
		if a.rec {
			recBadge = "REC ● "
		}
		msg := fmt.Sprintf("KEYS %s%s  z/q rows play  [ ] octave  ↑↓ velocity  tab voice  ^S record  esc close",
			recBadge, a.keysInstr)
		r.Text(1, 0, tui.Truncate(msg, r.W-2), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	case modeBeat:
		r.Fill(a.theme.HeaderBg)
		msg := fmt.Sprintf("BEAT %s  hjkl move  space toggle  +/- 1-9 0 vel  t/T voice|family  a/x track  enter apply+play  esc close",
			a.beat.name)
		r.Text(1, 0, tui.Truncate(msg, r.W-2), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	case modePrompt:
		r.TextField(a.editField, tui.TextFieldOpts{Prefix: a.promptLabel, Focused: true, Border: tui.LineNone})
	default:
		r.Fill(a.theme.HeaderBg)
		r.Text(1, 0, a.footHint(), a.theme.HintFg, a.theme.HeaderBg, terminal.AttrNone)
	}
}

// footHint names the keys that matter where the focus is; ? has the rest
func (a *tuiApp) footHint() string {
	switch a.focus {
	case focusInspector:
		return "j/k move  h/l fold  enter edit  a add  x del  space play  esc close  ? keys"
	case focusLog:
		return "j/k scroll  g/G ends  tab browser  : cmd  ? keys  ^Q quit"
	default: // focusBrowser
		if a.kind == kindPattern {
			return "j/k move  / find  space play  0-3 slot  enter inspect  b beat  p keys  : cmd  ? keys  ^Q quit"
		}
		return "j/k move  / find  space play  enter inspect  h/l patterns  p keys  : cmd  ? keys  ^Q quit"
	}
}
