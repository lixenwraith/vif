package main

// TUI shell over the same dispatch table the REPL uses. Every mutation and
// every audition goes through Execute — direct keybindings are macros that
// assemble a command line — so nothing the TUI can do is unreachable from a
// script, which is what keeps the scripted E2E authoritative.

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/audio"
)

type focusArea int

const (
	focusBrowser focusArea = iota
	focusInspector
	focusLog
	focusCount
)

type uiMode int

const (
	modeNormal uiMode = iota
	modeCommand
	modeEdit
	modePrompt // one-shot named-input line for n/c
	modePiano
	modeBeat
)

const (
	kindSound   = 0
	kindPattern = 1
	logPaneH    = 9
	pianoRowsH  = 2
	tickEvery   = 100 * time.Millisecond
)

type tuiApp struct {
	s     *Session
	term  terminal.Terminal
	log   *logBuffer
	theme tui.Theme

	w, h  int
	quit  bool
	focus focusArea
	mode  uiMode

	kind     int
	brCursor [2]int
	brScroll [2]int

	exp    *tui.TreeExpansion
	tree   *tui.TreeState
	nodes  []tui.TreeNode
	selKey string

	cmdField *tui.TextFieldState
	history  []string
	histIdx  int

	editField *tui.TextFieldState
	editPath  string

	promptLabel  string
	promptCommit func(string) // one-shot; consumed by promptKey
	promptReturn uiMode       // mode restored on close; chained prompts inherit it

	logScroll int

	pianoOct     int
	pianoVel     float64
	pianoInstr   audio.InstrumentType
	pianoLit     map[rune]int // keycap -> ticks of highlight remaining
	pianoStarted bool         // piano auto-started the transport; Esc stops it again

	rec      bool
	recSteps []audio.StepDef
	recInstr audio.InstrumentType
	recN     int

	beat *beatState

	quitArmed  bool   // one warning issued for unsaved-on-quit
	delArmed   string // browser 'd' pressed once for this name; second press deletes
	hintedFill bool   // slot-2 auto-fill hint shown once
}

func runTUI(s *Session) error {
	lb := newLogBuffer(500)
	s.out = lb
	// Close's wedged-mixer warning must reach a visible stream after the
	// alternate screen is gone. Deferred first, so it runs after Fini.
	defer func() { s.out = os.Stdout }()

	term := terminal.New()
	if err := term.Init(); err != nil {
		s.out = os.Stdout
		return err
	}
	defer term.Fini()

	// Signals become a clean loop exit rather than os.Exit: Fini must run or
	// the terminal is left raw on the alternate screen.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	sigDone := make(chan struct{})
	defer close(sigDone)
	go func() {
		select {
		case <-sig:
			term.PostEvent(terminal.Event{Type: terminal.EventClosed})
		case <-sigDone:
		}
	}()

	// The transport readout animates without input: a ticker wakes the
	// blocking PollEvent through the synthetic channel. KeyNone is what
	// unknown escape sequences already decode to, so the app tolerates it
	// by construction.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				term.PostEvent(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
			}
		}
	}()

	w, h := term.Size()
	a := &tuiApp{
		s: s, term: term, log: lb, theme: tui.DefaultTheme,
		w: w, h: h,
		exp:        tui.NewTreeExpansion(),
		tree:       tui.NewTreeState(10),
		cmdField:   tui.NewTextFieldState(""),
		editField:  tui.NewTextFieldState(""),
		pianoOct:   3,
		pianoVel:   0.8,
		pianoInstr: audio.InstrPiano,
		pianoLit:   map[rune]int{},
	}
	fmt.Fprintln(lb, "soundlab TUI — ':' commands, p piano, b beat(pattern), m/M music, ^S save, ^Q quit")
	if s.startErr != nil {
		fmt.Fprintf(lb, "audio backend: %v (silent mode)\n", s.startErr)
	}
	// An empty session has nothing to browse; the TUI seeds builtins per
	// empty document. REPL and scripts stay unseeded — they own their state.
	if len(s.sounds.order) == 0 {
		if err := s.seedBuiltinSounds(); err != nil {
			fmt.Fprintf(lb, "seed sounds: %v\n", err)
		}
	}
	if len(s.pats.order) == 0 {
		_ = s.seedBuiltinPatterns()
	}

	for !a.quit {
		a.render()
		a.handle(term.PollEvent())
	}
	return nil
}

// exec runs one command line, echoing it and any error into the log pane.
func (a *tuiApp) exec(line string) {
	fmt.Fprintf(a.log, "> %s\n", line)
	a.execQ(line)
}

// execQ runs without echoing the command — the piano would otherwise flood
// the log with one line per note. Errors still surface.
func (a *tuiApp) execQ(line string) {
	if err := Execute(a.s, line); err != nil {
		if errors.Is(err, errQuit) {
			a.requestQuit()
			return
		}
		fmt.Fprintf(a.log, "error: %v\n", err)
	}
	a.logScroll = 0 // new output re-sticks the log to its tail
}

// requestQuit guards unsaved work once, then quits. Applies to Ctrl+Q and
// :quit alike.
func (a *tuiApp) requestQuit() {
	if (a.s.sounds.modified || a.s.pats.modified) && !a.quitArmed {
		a.quitArmed = true
		fmt.Fprintln(a.log, "unsaved changes — ^S saves to provenance (:save sound|pattern <file> picks a path); quit again to discard")
		return
	}
	a.quit = true
}

// saveModified writes every document that diverges from disk. Provenance
// path when there is one; otherwise prompt for a file — erroring on a
// builtin/new-seeded document made ^S useless exactly when it matters.
func (a *tuiApp) saveModified() {
	if !a.s.sounds.modified && !a.s.pats.modified {
		fmt.Fprintln(a.log, "nothing to save — no document diverges from disk")
		return
	}
	if a.s.sounds.modified {
		if a.s.sounds.src == "" {
			a.promptSavePath("sound")
			return // resumes from the commit
		}
		a.exec("save sound")
	}
	if a.s.pats.modified {
		if a.s.pats.src == "" {
			a.promptSavePath("pattern")
			return
		}
		a.exec("save pattern")
	}
}

// promptSavePath asks for a first-save path, then resumes saveModified so
// the other document gets its turn (or its own prompt). A failed save
// leaves modified set, so the next pass re-prompts; Escape bails out.
func (a *tuiApp) promptSavePath(kind string) {
	a.openPrompt("save "+kind+" -> file: ", kind+"s.toml", func(file string) {
		a.exec("save " + kind + " " + file)
		if a.s.sounds.modified || a.s.pats.modified {
			a.saveModified()
		}
	})
}

// promptMix: '+' in the browser — pull the selection's tracks/layers into a
// named destination. cp's direction (selection = src) for consistency.
func (a *tuiApp) promptMix() {
	src := a.selName()
	if src == "" {
		return
	}
	if !addressable(src) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", src)
		return
	}
	a.openPrompt("mix "+src+" into: ", "", func(dst string) {
		a.exec("mix " + src + " " + dst)
		a.cursorTo(dst) // land on the combined entry
	})
}

func (a *tuiApp) handle(ev terminal.Event) {
	switch ev.Type {
	case terminal.EventResize:
		a.w, a.h = ev.Width, ev.Height
		return
	case terminal.EventClosed:
		a.quit = true // signal path: must exit so the deferred Fini runs
		return
	case terminal.EventError:
		fmt.Fprintf(a.log, "input error: %v\n", ev.Err)
		return
	case terminal.EventKey:
	default:
		return
	}
	if ev.Key == terminal.KeyNone {
		// Ticker wake (or unknown escape): decay piano highlights, render.
		for k, t := range a.pianoLit {
			if t <= 1 {
				delete(a.pianoLit, k)
			} else {
				a.pianoLit[k] = t - 1
			}
		}
		return
	}
	if ev.Key == terminal.KeyCtrlQ || ev.Key == terminal.KeyCtrlC {
		a.requestQuit()
		return
	}

	// Any other real key withdraws pending destructive confirms; 'd' must
	// survive this to be its own second press.
	a.quitArmed = false
	if ev.Key != terminal.KeyRune || ev.Rune != 'd' {
		a.delArmed = ""
	}

	switch a.mode {
	case modeCommand:
		a.commandKey(ev)
	case modeEdit:
		a.editKey(ev)
	case modePiano:
		a.pianoKey(ev)
	case modeBeat:
		a.beatKey(ev)
	case modePrompt:
		a.promptKey(ev)
	default:
		a.normalKey(ev)
	}
}

func (a *tuiApp) normalKey(ev terminal.Event) {
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	switch {
	case r == ':':
		a.mode = modeCommand
		a.cmdField.Clear()
		a.histIdx = len(a.history)
		return
	case r == 'p':
		a.enterPiano()
		return
	case r == 'm':
		a.exec("music reset")
		return
	case r == 'b':
		if a.kind == kindPattern {
			a.enterBeat()
		}
		return
	case ev.Key == terminal.KeyCtrlS:
		// Normal-mode save; the piano owns ^S for recording (mode-scoped),
		// which is why this is not a global intercept in handle().
		a.saveModified()
		return
	case ev.Key == terminal.KeyTab:
		a.focus = (a.focus + 1) % focusCount
		return
	}
	switch a.focus {
	case focusBrowser:
		a.browserKey(ev, r)
	case focusInspector:
		a.inspectorKey(ev, r)
	case focusLog:
		a.logKey(ev, r)
	}
}

// toggleMusic starts or stops the transport. Stop preserves the playhead by
// engine design (aligned resume); M / music reset zeroes it.
func (a *tuiApp) toggleMusic() {
	if a.s.eng.IsMusicPlaying() {
		a.exec("music stop")
		return
	}
	if a.s.eng.IsMusicMuted() {
		fmt.Fprintln(a.log, "music is muted — :mute none first")
		return
	}
	a.exec("music start")
	silent := true
	for i := range 3 {
		if a.s.eng.SlotPattern(i) != audio.PatternSilence {
			silent = false
			break
		}
	}
	if silent {
		fmt.Fprintln(a.log, "all slots silent — 0/1/2 on a pattern assigns it")
	}
}

func (a *tuiApp) browserKey(ev terminal.Event, r rune) {
	ns := a.browserNames()
	c := &a.brCursor[a.kind]
	switch {
	case ev.Key == terminal.KeyDown || r == 'j':
		*c++
	case ev.Key == terminal.KeyUp || r == 'k':
		*c--
	case r == 'g':
		*c = 0
	case r == 'G':
		*c = len(ns) - 1
	case r == 'n':
		a.promptNew()
	case r == 'c':
		a.promptClone()
	case r == 'w':
		a.promptWrite()
	case r == '+' || r == '=':
		a.promptMix()
	case r == 'd':
		a.deleteSel()
	case ev.Key == terminal.KeyLeft || ev.Key == terminal.KeyRight || r == 'h' || r == 'l':
		a.kind ^= 1
	case ev.Key == terminal.KeyEnter:
		if a.selName() != "" {
			a.focus = focusInspector
		}
	case ev.Key == terminal.KeySpace || r == ' ':
		a.auditionSel()
	case r >= '0' && r <= '2':
		a.assignSlot(int(r - '0'))
	case r == 'a':
		a.nameCmd("apply")
	case r == 'v':
		a.nameCmd("validate")
	case r == 'r':
		a.nameCmd("revert")
	}
	ns = a.browserNames()
	if *c > len(ns)-1 {
		*c = len(ns) - 1
	}
	if *c < 0 {
		*c = 0
	}
}

// deleteSel arms-then-deletes the selection, quitArmed-style. The arm
// message states what survives: del is always document-level — an applied
// entry keeps its registry copy until process exit; an entry that was never
// applied or saved exists only here and the discard is final.
func (a *tuiApp) deleteSel() {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	if a.delArmed != n {
		a.delArmed = n
		where := "unapplied and unsaved — this discard is final"
		if (a.kind == kindSound && audio.SoundDefByName(n) != nil) ||
			(a.kind == kindPattern && audio.PatternIDByName(n) != audio.PatternSilence) {
			where = "registry keeps the applied copy (load/builtin to re-import)"
		}
		fmt.Fprintf(a.log, "delete %q from document? %s — d again to confirm\n", n, where)
		return
	}
	a.delArmed = ""
	a.exec("del " + n)
}

func (a *tuiApp) audition() {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	a.exec("play " + n)
}

// auditionSel is the space action, shared by browser and inspector: sounds
// audition immediately; patterns ensure they are audible.
func (a *tuiApp) auditionSel() {
	if a.kind == kindSound {
		a.audition()
		return
	}
	a.playPattern()
}

// playPattern makes space mean "hear this": already in a slot → transport
// play/pause; otherwise lowest empty slot via assignSlot (auto-apply,
// auto-start, fill hint). SlotPattern is the sequencer-published readout —
// one step stale at worst; the race outcome is a same-slot re-assign or a
// transport toggle, both benign.
func (a *tuiApp) playPattern() {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	if id := audio.PatternIDByName(n); id != audio.PatternSilence {
		for i := range 3 {
			if a.s.eng.SlotPattern(i) == id {
				a.toggleMusic()
				return
			}
		}
	}
	for i := range 3 {
		if a.s.eng.SlotPattern(i) == audio.PatternSilence {
			a.assignSlot(i)
			return
		}
	}
	fmt.Fprintln(a.log, "all slots full — 0-2 replaces a slot (same key again clears it)")
}

// assignSlot toggles the cursor pattern in a slot: assigning the pattern a
// slot already holds clears the slot instead, so one key both layers and
// un-layers. Assignment auto-starts the transport — the pattern must sound.
func (a *tuiApp) assignSlot(slot int) {
	if a.kind != kindPattern {
		return
	}
	n := a.selName()
	if n == "" || !addressable(n) {
		return
	}
	if id := audio.PatternIDByName(n); id != audio.PatternSilence {
		if a.s.eng.SlotPattern(slot) == id {
			a.exec(fmt.Sprintf("slot %d -", slot))
			return
		}
		// Enforce exclusivity: clear from any other active slots
		for i := range 3 {
			if i != slot && a.s.eng.SlotPattern(i) == id {
				a.exec(fmt.Sprintf("slot %d -", i))
			}
		}
	}
	a.exec(fmt.Sprintf("slot %d %s", slot, n))
	if slot == 2 && !a.hintedFill {
		a.hintedFill = true
		fmt.Fprintln(a.log, "note: slot 2 auto-fill swaps once per phrase — :fill off to pin")
	}
	if !a.s.eng.IsMusicPlaying() {
		if a.s.eng.IsMusicMuted() {
			fmt.Fprintln(a.log, "music is muted — :mute none to hear it")
			return
		}
		a.exec("music start")
	}
}

func (a *tuiApp) nameCmd(verb string) {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	a.exec(verb + " " + n)
}

func (a *tuiApp) inspectorKey(ev terminal.Event, r rune) {
	total := len(a.nodes)
	m, full := a.curMeta()
	switch {
	case ev.Key == terminal.KeyDown || r == 'j':
		a.tree.MoveCursor(1, total)
	case ev.Key == terminal.KeyUp || r == 'k':
		a.tree.MoveCursor(-1, total)
	case r == 'g':
		a.tree.JumpStart()
	case r == 'G':
		a.tree.JumpEnd(total)
	case ev.Key == terminal.KeyRight || r == 'l':
		if m != nil && a.curExpandable() {
			a.exp.Expand(full)
		}
	case r == 'L':
		a.expandAll()
	case ev.Key == terminal.KeyLeft || r == 'h':
		if m != nil {
			a.exp.Collapse(full)
		}
	case r == 'H':
		a.collapseAll()
	case ev.Key == terminal.KeyEnter:
		a.inspectorEnter(m, full)
	case r == 'a':
		a.inspectorAdd(m)
	case r == 'x':
		if m != nil && m.elem {
			a.exec("del " + full)
		}
	case ev.Key == terminal.KeySpace || r == ' ':
		a.auditionSel()
	case ev.Key == terminal.KeyEscape:
		a.focus = focusBrowser
	case ev.Key == terminal.KeyEscape:
		a.focus = focusBrowser
	}
}

// expandAll runs to fixpoint: children exist in the node list only after
// their parent expands, so each pass can reveal new expandables. Depth is
// bounded by the spec shape (~5); 8 passes is slack.
func (a *tuiApp) expandAll() {
	name := a.selName()
	if name == "" {
		return
	}
	for range 8 {
		a.buildNodes(name)
		changed := false
		for i := range a.nodes {
			n := &a.nodes[i]
			if n.Expandable && !a.exp.IsExpanded(n.Key) {
				a.exp.Expand(n.Key)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}

func (a *tuiApp) collapseAll() {
	for i := range a.nodes {
		if a.nodes[i].Expandable {
			a.exp.Collapse(a.nodes[i].Key)
		}
	}
	a.tree.JumpStart()
}

func (a *tuiApp) inspectorEnter(m *nodeMeta, full string) {
	switch {
	case m == nil:
	case m.ro:
		fmt.Fprintln(a.log, "name is the registration identity; rename with :mv")
	case m.leaf:
		a.startEdit(m)
	case m.slice && !a.curExpandable(): // empty list: Enter appends
		a.inspectorAdd(m)
	default:
		a.exp.Toggle(full)
	}
}

func (a *tuiApp) inspectorAdd(m *nodeMeta) {
	if m == nil {
		return
	}
	name := a.selName()
	var segs []string
	switch {
	case m.slice:
		segs = m.segs
	case m.elem:
		segs = m.segs[:len(m.segs)-1] // append to the element's parent list
	default:
		fmt.Fprintln(a.log, "a: cursor a list or a list element to append")
		return
	}
	p := name + "." + strings.Join(segs, ".")
	a.exec("add " + p)
	a.exp.Expand(p)
}

func (a *tuiApp) startEdit(m *nodeMeta) {
	name := a.selName()
	a.editPath = name + "." + strings.Join(m.segs, ".")
	a.editField.SetValue(a.leafPrefill(name, m.segs))
	a.mode = modeEdit
}

func (a *tuiApp) logKey(ev terminal.Event, r rune) {
	switch {
	case ev.Key == terminal.KeyDown || r == 'j':
		a.logScroll--
	case ev.Key == terminal.KeyUp || r == 'k':
		a.logScroll++
	case ev.Key == terminal.KeyPageDown:
		a.logScroll -= 5
	case ev.Key == terminal.KeyPageUp:
		a.logScroll += 5
	case r == 'G':
		a.logScroll = 0
	case r == 'g':
		a.logScroll = 1 << 20 // clamped to the top at render
	}
	if a.logScroll < 0 {
		a.logScroll = 0
	}
}

func (a *tuiApp) commandKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEnter:
		line := strings.TrimSpace(a.cmdField.Value())
		a.mode = modeNormal
		if line == "" {
			return
		}
		if n := len(a.history); n == 0 || a.history[n-1] != line {
			a.history = append(a.history, line)
		}
		a.histIdx = len(a.history)
		a.exec(line)
	case terminal.KeyEscape:
		a.mode = modeNormal
	case terminal.KeyUp:
		if a.histIdx > 0 {
			a.histIdx--
			a.cmdField.SetValue(a.history[a.histIdx])
		}
	case terminal.KeyDown:
		if a.histIdx < len(a.history) {
			a.histIdx++
			if a.histIdx == len(a.history) {
				a.cmdField.SetValue("")
			} else {
				a.cmdField.SetValue(a.history[a.histIdx])
			}
		}
	default:
		a.cmdField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

func (a *tuiApp) editKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEnter:
		val := strings.TrimSpace(a.editField.Value())
		a.mode = modeNormal
		if val == "" {
			fmt.Fprintf(a.log, "empty value; clear a field with :unset %s\n", a.editPath)
			return
		}
		a.exec("set " + a.editPath + " " + val)
	case terminal.KeyEscape:
		a.mode = modeNormal
	default:
		a.editField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// openPrompt arms a one-shot input line: label as the field prefix, commit
// invoked with the trimmed value on Enter. Reuses editField — prompt and
// edit modes are mutually exclusive by construction.
func (a *tuiApp) openPrompt(label, prefill string, commit func(string)) {
	a.promptLabel = label
	a.promptCommit = commit
	a.promptReturn = a.mode // beat's ^S must land back in the grid
	a.editField.SetValue(prefill)
	a.mode = modePrompt
}

func (a *tuiApp) promptKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEnter:
		val := strings.TrimSpace(a.editField.Value())
		commit := a.promptCommit
		// Restore before commit: a commit that opens the next prompt
		// (chained saveModified) must capture the original return mode.
		a.mode, a.promptCommit = a.promptReturn, nil
		if val == "" || commit == nil {
			return
		}
		commit(val)
	case terminal.KeyEscape:
		a.mode, a.promptCommit = a.promptReturn, nil
	default:
		a.editField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// promptNew: 'n' in the browser. Kind is captured at open so a later tab
// switch cannot retarget the command.
func (a *tuiApp) promptNew() {
	kind := "sound"
	if a.kind == kindPattern {
		kind = "pattern"
	}
	a.openPrompt("new "+kind+" name: ", "", func(name string) {
		a.exec("new " + kind + " " + name)
		a.cursorTo(name) // no-op when new failed: the entry does not exist
	})
}

// promptClone: 'c' in the browser, prefilled with the source name — the
// common case is a suffix edit. Commit lands the cursor on the copy and
// opens it in the inspector with the primary list pre-expanded: the
// clone-and-tweak flow without the Enter+l hop.
func (a *tuiApp) promptClone() {
	src := a.selName()
	if src == "" {
		return
	}
	if !addressable(src) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", src)
		return
	}
	a.openPrompt("cp "+src+" -> ", src, func(dst string) {
		a.exec("cp " + src + " " + dst)
		if !a.cursorTo(dst) {
			return // cp failed (exists / bad name); error already logged
		}
		a.focus = focusInspector
		list := "layer"
		if a.kind == kindPattern {
			list = "track"
		}
		a.exp.Expand(dst + "." + list)
	})
}

// promptWrite: 'w' in the browser — standalone single-entry export of the
// selection. Prefilled with <name>.toml; paths with spaces are outside the
// command grammar, same as everywhere else.
func (a *tuiApp) promptWrite() {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	a.openPrompt("write "+n+" -> file: ", n+".toml", func(file string) {
		a.exec("write " + n + " " + file)
	})
}
