//go:build !vif_headless && !vif_noaudio && !wasm

package main

// TUI shell over the same dispatch table the REPL uses. Every mutation and
// every audition goes through Execute — direct keybindings are macros that
// assemble a command line — so nothing the TUI can do is unreachable from a
// script, which is what keeps the scripted E2E authoritative.

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/pkg/audio"
)

// focusArea is the pane the keys go to; Tab walks the panes on screen in this order
type focusArea int

const (
	focusMain focusArea = iota // the view's own pane: the browser, the keyboard or the grid
	focusMixer
	focusLog
	focusInspector // the browser's overlay: Enter opens it, Esc closes it
)

// view is what the body shows: the browser with the mixer and log, or one of the
// two editors that take the body over
type view int

const (
	viewBrowse view = iota
	viewKeys
	viewBeat
)

// uiMode is where typed keys go: the view, or a line being typed in the footer
type uiMode int

const (
	modeNormal uiMode = iota
	modeCommand
	modeEdit
	modePrompt // one-shot named-input line for n/c/w/+ and first saves
)

const (
	kindPattern = 0
	kindSound   = 1
	tickEvery   = 100 * time.Millisecond
	// minW and minH fit the keys view's drawn keyboard and the beat grid's bar
	minW, minH = 60, 16
)

// palette is the shell's colors by role
type palette struct {
	Bg, Fg, FocusBg, CursorBg, HeaderBg, HeaderFg color.RGB
	Selected, Unselected, Warning, Error          color.RGB
	Border, HintFg                                color.RGB
	Ivory, Ebony, KeyLine                         color.RGB
}

var defaultPalette = palette{
	Bg: color.RGB{R: 20, G: 20, B: 30}, Fg: color.RGB{R: 200, G: 200, B: 200},
	FocusBg: color.RGB{R: 30, G: 35, B: 45}, CursorBg: color.RGB{R: 50, G: 50, B: 70},
	HeaderBg: color.RGB{R: 40, G: 60, B: 90}, HeaderFg: color.RGB{R: 255, G: 255, B: 255},
	Selected: color.RGB{R: 80, G: 200, B: 80}, Unselected: color.RGB{R: 100, G: 100, B: 100},
	Warning: color.RGB{R: 255, G: 180, B: 60}, Error: color.RGB{R: 255, G: 80, B: 80},
	Border: color.RGB{R: 60, G: 80, B: 100}, HintFg: color.RGB{R: 100, G: 180, B: 200},
	Ivory: color.RGB{R: 225, G: 222, B: 210}, Ebony: color.RGB{R: 34, G: 34, B: 40},
	KeyLine: color.RGB{R: 120, G: 118, B: 110},
}

// engineSig is the engine state the screen shows; a tick redraws only when it moved
type engineSig struct {
	bar        int64
	step       int
	running    bool
	slots      [audio.MusicSlots]audio.PatternID
	muted      [audio.MusicSlots]bool
	backend    string
	silent     bool
	musicMuted bool
}

type tuiApp struct {
	s     *Session
	term  terminal.Terminal
	log   *logBuffer
	theme palette

	w, h  int
	quit  bool
	dirty bool // something changed since the last frame
	sig   engineSig
	cells []terminal.Cell

	view  view
	focus focusArea
	panes []focusArea // the panes the last frame laid out, in Tab order
	mode  uiMode

	kind     int
	brCursor [2]int
	brScroll [2]int
	brPageH  int    // rows the browser showed last render, a page for PgUp/PgDn
	search   string // the last / term, prefilled so / again finds the next

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

	logScroll int

	// The mixer's cursor and first row shown, as rows, so a slot changing what it
	// plays leaves them on the track they were on
	mixCur, mixTop mixRow
	mixPageH       int

	keys keysState
	beat *beatState

	quitArmed  bool   // one warning issued for unsaved-on-quit
	delArmed   string // browser 'd' pressed once for this name; second press deletes
	hintedFill bool   // slot-2 auto-fill hint shown once
	help       bool   // the key overlay is up; any key but j/k takes it down
	helpTop    int    // the overlay's first row shown
}

func newTUIApp(s *Session, lb *logBuffer) *tuiApp {
	return &tuiApp{
		s: s, log: lb, theme: defaultPalette, dirty: true,
		exp:       tui.NewTreeExpansion(),
		tree:      tui.NewTreeState(10),
		cmdField:  tui.NewTextFieldState(""),
		editField: tui.NewTextFieldState(""),
		mixCur:    mixRow{trk: -1},
		mixTop:    mixRow{trk: -1},
		keys:      newKeysState(),
	}
}

// runTUI takes over the terminal; lb is the log the session already writes to
func runTUI(s *Session, lb *logBuffer) error {
	// Close's wedged-mixer warning must reach a visible stream after the
	// alternate screen is gone. Deferred first, so it runs after Fini.
	defer func() { s.out = os.Stdout }()

	term := terminal.New()
	if err := term.Init(); err != nil {
		for _, l := range lb.lines {
			fmt.Println(l)
		}
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
	// blocking PollEvent with KeyNone, which unknown escape sequences already
	// decode to, so the app tolerates it by construction.
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

	a := newTUIApp(s, lb)
	a.term = term
	a.w, a.h = term.Size()
	fmt.Fprintln(lb, "soundlab — p keys, b beat editor, : commands, ? keys, ^S save, ^Q quit")
	// An empty session has nothing to browse; the TUI seeds builtins per
	// empty document. REPL and scripts stay unseeded — they own their state.
	if len(s.pats.order) == 0 {
		_ = s.seedBuiltinPatterns()
	}
	if len(s.sounds.order) == 0 {
		if err := s.seedBuiltinSounds(); err != nil {
			fmt.Fprintf(lb, "seed sounds: %v\n", err)
		}
	}

	for !a.quit {
		if a.dirty {
			a.dirty = false
			a.render()
		}
		a.handle(term.PollEvent())
	}
	return nil
}

// tooSmall is a terminal the views cannot be laid out in; it shows a notice alone
func (a *tuiApp) tooSmall() bool { return a.w < minW || a.h < minH }

// exec runs one command line, echoing it and any error into the log pane, and
// returns the error for a caller whose next step depends on it.
func (a *tuiApp) exec(line string) error {
	fmt.Fprintf(a.log, "> %s\n", line)
	return a.execQ(line)
}

// execQ runs without echoing the command — the keys would otherwise flood
// the log with one line per note. Errors still surface.
func (a *tuiApp) execQ(line string) error {
	a.logScroll = 0 // new output re-sticks the log to its tail
	err := Execute(a.s, line)
	switch {
	case errors.Is(err, errQuit):
		a.requestQuit()
	case err != nil:
		fmt.Fprintf(a.log, "error: %v\n", err)
	}
	return err
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

// saveModified writes every document that diverges from disk: to its provenance
// path, or, the first time, to a path a prompt asks for, offering the file the
// next vif run reads first.
func (a *tuiApp) saveModified() {
	if !a.s.sounds.modified && !a.s.pats.modified {
		fmt.Fprintln(a.log, "nothing to save — no document diverges from disk")
		return
	}
	if a.s.pats.modified {
		if a.s.pats.src == "" {
			a.promptSavePath("pattern", a.s.files.musicTarget)
			return // resumes from the commit
		}
		a.exec("save pattern")
	}
	if a.s.sounds.modified {
		if a.s.sounds.src == "" {
			a.promptSavePath("sound", a.s.files.soundsTarget)
			return
		}
		a.exec("save sound")
	}
}

// promptSavePath asks for a first-save path, then resumes saveModified so
// the other document gets its turn (or its own prompt). A failed save
// leaves modified set, so the next pass re-prompts; Escape bails out.
func (a *tuiApp) promptSavePath(kind, target string) {
	a.openPrompt("save "+kind+" -> file: ", cmp.Or(target, kind+"s.toml"), func(file string) {
		a.exec("save " + kind + " " + file)
		if a.s.sounds.modified || a.s.pats.modified {
			a.saveModified()
		}
	})
}

// promptMix: '+' in the browser — pull the selection's tracks/layers into a
// named destination. cp's direction (selection = src) for consistency.
func (a *tuiApp) promptMix() {
	src, ok := a.addressableSel()
	if !ok {
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
		a.dirty = true
		return
	case terminal.EventClosed:
		a.quit = true // signal path: must exit so the deferred Fini runs
		return
	case terminal.EventError:
		fmt.Fprintf(a.log, "input error: %v\n", ev.Err)
		a.dirty = true
		return
	case terminal.EventKey:
	default:
		return
	}
	if ev.Key == terminal.KeyNone {
		a.tick()
		return
	}
	if ev.Key == terminal.KeyCtrlQ || ev.Key == terminal.KeyCtrlC {
		a.requestQuit()
		a.dirty = true
		return
	}
	if a.tooSmall() {
		return // nothing it would act on is on screen, and nothing redraws
	}
	a.dirty = true
	if a.help {
		switch {
		case ev.Key == terminal.KeyDown || ev.Key == terminal.KeyRune && ev.Rune == 'j':
			a.helpTop++
		case ev.Key == terminal.KeyUp || ev.Key == terminal.KeyRune && ev.Rune == 'k':
			a.helpTop = max(a.helpTop-1, 0)
		default:
			a.help, a.helpTop = false, 0
		}
		return
	}

	// Any other real key withdraws pending destructive confirms; 'd' must
	// survive this to be its own second press (the beat view keeps its own 'x').
	a.quitArmed = false
	if ev.Key != terminal.KeyRune || ev.Rune != 'd' {
		a.delArmed = ""
	}

	switch a.mode {
	case modeCommand:
		a.commandKey(ev)
	case modeEdit:
		a.editKey(ev)
	case modePrompt:
		a.promptKey(ev)
	default:
		if a.globalKey(ev) {
			return
		}
		switch a.view {
		case viewKeys:
			a.keysKey(ev)
		case viewBeat:
			a.beatKey(ev)
		default:
			a.normalKey(ev)
		}
	}
}

// globalKey takes the keys that mean one thing in every view and pane: Tab and
// Shift-Tab walk the panes on screen, Esc brings the keys back from a side pane to
// the view's own, ? shows the keys, : opens the command line, ^S saves and < >
// step the tempo
func (a *tuiApp) globalKey(ev terminal.Event) bool {
	switch {
	case ev.Key == terminal.KeyTab || ev.Key == terminal.KeyBacktab:
		a.cycleFocus(ev.Key == terminal.KeyTab)
	case ev.Key == terminal.KeyEscape && (a.focus == focusMixer || a.focus == focusLog):
		a.focus = focusMain
	case ev.Key == terminal.KeyCtrlS:
		a.saveModified()
	case ev.Key == terminal.KeyRune && ev.Rune == '?':
		a.help = true
	case ev.Key == terminal.KeyRune && ev.Rune == ':':
		a.openCommand()
	case ev.Key == terminal.KeyRune && (ev.Rune == '<' || ev.Rune == '>'):
		d := 5
		if ev.Rune == '<' {
			d = -5
		}
		a.exec(fmt.Sprintf("bpm %d", a.s.bpm+d))
	default:
		return false
	}
	return true
}

// cycleFocus moves the keys to the next pane on screen, or the previous; the
// inspector counts as the browser it opened over
func (a *tuiApp) cycleFocus(next bool) {
	n := len(a.panes)
	if n == 0 {
		return
	}
	d := 1
	if !next {
		d = n - 1
	}
	a.focus = a.panes[(max(slices.Index(a.panes, a.focus), 0)+d)%n]
}

// tick decays the keys' highlights and redraws only when the engine moved, so an
// idle screen, or one too small to show anything, is left alone
func (a *tuiApp) tick() {
	if a.tooSmall() {
		return
	}
	if a.keys.decay() || a.engineSig() != a.sig {
		a.dirty = true
	}
}

func (a *tuiApp) engineSig() engineSig {
	e := a.s.eng
	g := engineSig{backend: e.BackendName(), silent: e.IsSilent(), musicMuted: e.IsMusicMuted()}
	g.bar, g.step, g.running = e.Transport()
	for i := range g.slots {
		g.slots[i], g.muted[i] = e.SlotPattern(i), e.IsSlotMuted(i)
	}
	return g
}

// openCommand opens the command line in any view; Enter runs it there
func (a *tuiApp) openCommand() {
	a.mode = modeCommand
	a.cmdField.Clear()
	a.histIdx = len(a.history)
}

func (a *tuiApp) normalKey(ev terminal.Event) {
	r := rune(0)
	if ev.Key == terminal.KeyRune {
		r = ev.Rune
	}
	switch {
	case r == 'p':
		a.enterKeys()
		return
	case r == 'b':
		if a.kind == kindPattern {
			a.enterBeat()
		} else {
			fmt.Fprintln(a.log, "the beat editor edits a pattern — h/l to the patterns tab first")
		}
		return
	}
	switch a.focus {
	case focusMain:
		a.browserKey(ev, r)
	case focusInspector:
		a.inspectorKey(ev, r)
	case focusMixer:
		a.mixerKey(ev, r)
	case focusLog:
		a.logKey(ev, r)
	}
}

// toggleMusic starts or stops the transport. Stop preserves the playhead by
// engine design (aligned resume); m / music reset zeroes it.
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
	for i := range audio.MusicSlots {
		if a.s.eng.SlotPattern(i) != audio.PatternSilence {
			silent = false
			break
		}
	}
	if silent {
		fmt.Fprintln(a.log, "all slots silent — 0-3 on a pattern assigns it")
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
	case ev.Key == terminal.KeyPageDown:
		*c += max(a.brPageH-1, 1)
	case ev.Key == terminal.KeyPageUp:
		*c -= max(a.brPageH-1, 1)
	case r == 'g' || ev.Key == terminal.KeyHome:
		*c = 0
	case r == 'G' || ev.Key == terminal.KeyEnd:
		*c = len(ns) - 1
	case r == '/':
		a.promptSearch()
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
	case r >= '0' && r < '0'+audio.MusicSlots:
		a.assignSlot(int(r - '0'))
	case r == 'a':
		a.nameCmd("apply")
	case r == 'm':
		a.exec("music reset")
	case r == 'v':
		a.nameCmd("validate")
	case r == 'r':
		a.nameCmd("revert")
	}
	ns = a.browserNames()
	*c = min(max(*c, 0), max(len(ns)-1, 0))
}

// deleteSel arms-then-deletes the selection, quitArmed-style. The arm message
// states what survives: del is document-level, so an applied entry keeps its
// registry copy; one never applied or saved exists only here.
func (a *tuiApp) deleteSel() {
	n, ok := a.addressableSel()
	if !ok {
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

// auditionSel is the space action, shared by browser and inspector: sounds
// audition immediately; patterns ensure they are audible.
func (a *tuiApp) auditionSel() {
	if a.kind == kindPattern {
		a.playPattern()
		return
	}
	if n, ok := a.addressableSel(); ok {
		a.exec("play " + n)
	}
}

// playPattern makes space mean "hear this": already in a slot, it toggles the
// transport; otherwise it takes its role's slot, or the lowest empty one, through
// assignSlot (auto-apply, auto-start, fill hint).
func (a *tuiApp) playPattern() {
	n, ok := a.addressableSel()
	if !ok {
		return
	}
	if id := audio.PatternIDByName(n); id != audio.PatternSilence && a.slotOf(id) >= 0 {
		a.toggleMusic()
		return
	}
	if slot := a.roleSlot(n); slot >= 0 && a.s.eng.SlotPattern(slot) == audio.PatternSilence {
		a.assignSlot(slot)
		return
	}
	for i := range audio.MusicSlots {
		if a.s.eng.SlotPattern(i) == audio.PatternSilence {
			a.assignSlot(i)
			return
		}
	}
	fmt.Fprintln(a.log, "all slots full — 0-3 replaces a slot (same key again clears it)")
}

// slotOf is the slot sounding id, -1 for none
func (a *tuiApp) slotOf(id audio.PatternID) int {
	for i := range audio.MusicSlots {
		if a.s.eng.SlotPattern(i) == id {
			return i
		}
	}
	return -1
}

// roleSlot is the slot the game draws a pattern's role into, -1 for a pattern
// without one
func (a *tuiApp) roleSlot(name string) int {
	if d, ok := a.s.pats.get(name); ok {
		for i, r := range slotRoles {
			if d.Role == r {
				return i
			}
		}
	}
	return -1
}

// assignSlot toggles the cursor pattern in a slot: assigning the pattern a slot
// already holds clears it, so one key both layers and un-layers, and clearing the
// last sounding slot stops the transport. Assignment starts the transport, and
// while it runs lands on the next bar, so the pattern enters in step.
func (a *tuiApp) assignSlot(slot int) {
	if a.kind != kindPattern {
		fmt.Fprintln(a.log, "slots take patterns — space auditions a sound")
		return
	}
	n, ok := a.addressableSel()
	if !ok {
		return
	}
	a.placeInSlot(slot, n)
}

// placeInSlot puts a pattern in a slot, out of any other, and starts the transport
func (a *tuiApp) placeInSlot(slot int, n string) {
	if id := audio.PatternIDByName(n); id != audio.PatternSilence {
		if a.s.eng.SlotPattern(slot) == id {
			a.exec(fmt.Sprintf("slot %d -", slot))
			a.stopIfClear(slot)
			return
		}
		for i := range audio.MusicSlots {
			if i != slot && a.s.eng.SlotPattern(i) == id {
				a.exec(fmt.Sprintf("slot %d -", i))
			}
		}
	}
	if a.s.eng.IsMusicPlaying() {
		a.exec(fmt.Sprintf("slot %d %s 250 q", slot, n))
	} else {
		a.exec(fmt.Sprintf("slot %d %s", slot, n))
	}
	if slot == 2 && !a.hintedFill {
		a.hintedFill = true
		fmt.Fprintln(a.log, "note: with :fill on, slot 2 auto-fill swaps once per phrase — :fill off to pin")
	}
	if !a.s.eng.IsMusicPlaying() {
		if a.s.eng.IsMusicMuted() {
			fmt.Fprintln(a.log, "music is muted — :mute none to hear it")
			return
		}
		a.exec("music start")
	}
}

// stopIfClear stops the transport once no slot but the one just cleared holds a
// pattern, so a bar count never runs on over silence; m still resets it.
func (a *tuiApp) stopIfClear(cleared int) {
	for i := range audio.MusicSlots {
		if i != cleared && a.s.eng.SlotPattern(i) != audio.PatternSilence {
			return
		}
	}
	if a.s.eng.IsMusicPlaying() {
		a.exec("music stop")
		fmt.Fprintln(a.log, "every slot clear — transport stopped; m resets the bar count")
	}
}

func (a *tuiApp) nameCmd(verb string) {
	if n, ok := a.addressableSel(); ok {
		a.exec(verb + " " + n)
	}
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
		a.focus = focusMain
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
	p := a.selName() + "." + strings.Join(segs, ".")
	a.exec("add " + p)
	a.exp.Expand(p)
}

func (a *tuiApp) startEdit(m *nodeMeta) {
	name := a.selName()
	a.editPath = name + "." + strings.Join(m.segs, ".")
	a.editField.SetValue(a.leafPrefill(name, m.segs))
	a.mode = modeEdit
}

// mixerKey moves the mixer's cursor over its rows, each slot's title and tracks;
// the rows shown follow it, and stay where it leaves them
func (a *tuiApp) mixerKey(ev terminal.Event, r rune) {
	rows := a.mixRows()
	i := mixIndex(rows, a.mixCur)
	switch {
	case ev.Key == terminal.KeyDown || r == 'j':
		i++
	case ev.Key == terminal.KeyUp || r == 'k':
		i--
	case ev.Key == terminal.KeyPageDown:
		i += max(a.mixPageH-1, 1)
	case ev.Key == terminal.KeyPageUp:
		i -= max(a.mixPageH-1, 1)
	case r == 'g' || ev.Key == terminal.KeyHome:
		i = 0
	case r == 'G' || ev.Key == terminal.KeyEnd:
		i = len(rows) - 1
	case ev.Key == terminal.KeySpace || r == ' ' || r == 'm':
		slot, mute := a.mixCur.slot, "on"
		if a.s.eng.IsSlotMuted(slot) {
			mute = "off"
		}
		a.exec(fmt.Sprintf("mute slot %d %s", slot, mute))
	}
	a.mixCur = rows[min(max(i, 0), len(rows)-1)]
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
	a.logScroll = max(a.logScroll, 0)
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
	a.editField.SetValue(prefill)
	a.mode = modePrompt
}

func (a *tuiApp) promptKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEnter:
		val := strings.TrimSpace(a.editField.Value())
		commit := a.promptCommit
		// Close before commit: a commit may open the next prompt (chained saves)
		a.mode, a.promptCommit = modeNormal, nil
		if val == "" || commit == nil {
			return
		}
		commit(val)
	case terminal.KeyEscape:
		a.mode, a.promptCommit = modeNormal, nil
	default:
		a.editField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// promptSearch: '/' in the browser. The next name after the cursor containing the
// term, case-insensitive and wrapping; the term stays prefilled, so / and Enter
// again walk the matches.
func (a *tuiApp) promptSearch() {
	a.openPrompt("search: ", a.search, func(term string) {
		a.search = term
		ns := a.browserNames()
		c := &a.brCursor[a.kind]
		for i := 1; i <= len(ns); i++ {
			if j := (*c + i) % len(ns); strings.Contains(strings.ToLower(ns[j]), strings.ToLower(term)) {
				*c = j
				return
			}
		}
		fmt.Fprintf(a.log, "no name contains %q\n", term)
	})
}

// promptNew: 'n' in the browser. Kind is captured at open so a later tab
// switch cannot retarget the command.
func (a *tuiApp) promptNew() {
	kind := kindWord(a.kind)
	a.openPrompt("new "+kind+" name: ", "", func(name string) {
		a.exec("new " + kind + " " + name)
		a.cursorTo(name) // no-op when new failed: the entry does not exist
	})
}

// promptClone: 'c' in the browser, prefilled with the source name — the
// common case is a suffix edit. Commit lands the cursor on the copy and
// opens it in the inspector with the primary list pre-expanded.
func (a *tuiApp) promptClone() {
	src, ok := a.addressableSel()
	if !ok {
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
// selection, prefilled with <name>.toml.
func (a *tuiApp) promptWrite() {
	n, ok := a.addressableSel()
	if !ok {
		return
	}
	a.openPrompt("write "+n+" -> file: ", n+".toml", func(file string) {
		a.exec("write " + n + " " + file)
	})
}

func kindWord(kind int) string {
	if kind == kindPattern {
		return "pattern"
	}
	return "sound"
}
