package main

// TUI shell over the same dispatch table the REPL uses. Every mutation and
// every audition goes through Execute — the direct keybindings are macros
// that assemble a command line — so nothing the TUI can do is unreachable
// from a script, which is what keeps the scripted E2E authoritative.

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
)

const (
	kindSound   = 0
	kindPattern = 1
	logPaneH    = 9
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

	kind     int    // browser tab: kindSound | kindPattern
	brCursor [2]int // per-kind cursor survives tab switches
	brScroll [2]int

	exp    *tui.TreeExpansion
	tree   *tui.TreeState
	nodes  []tui.TreeNode
	selKey string // kind/name; change resets the tree cursor

	cmdField *tui.TextFieldState
	history  []string
	histIdx  int

	editField *tui.TextFieldState
	editPath  string // full "name.a.b" for the pending set

	logScroll int // lines up from the tail; 0 = follow
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
	// the terminal is left in raw mode on the alternate screen.
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
	// unknown escape sequences already decode to, so the app tolerates it by
	// construction. This replaces the demo's poll-with-default spin.
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
		exp:       tui.NewTreeExpansion(),
		tree:      tui.NewTreeState(10),
		cmdField:  tui.NewTextFieldState(""),
		editField: tui.NewTextFieldState(""),
	}
	fmt.Fprintln(lb, "soundlab TUI — ':' for commands, Tab cycles focus, Ctrl+Q quits")
	if s.startErr != nil {
		fmt.Fprintf(lb, "audio backend: %v (silent mode)\n", s.startErr)
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
	if err := Execute(a.s, line); err != nil {
		if errors.Is(err, errQuit) {
			a.quit = true
			return
		}
		fmt.Fprintf(a.log, "error: %v\n", err)
	}
	a.logScroll = 0 // new output re-sticks the log to its tail
}

func (a *tuiApp) handle(ev terminal.Event) {
	switch ev.Type {
	case terminal.EventResize:
		a.w, a.h = ev.Width, ev.Height
		return
	case terminal.EventClosed:
		a.quit = true
		return
	case terminal.EventError:
		fmt.Fprintf(a.log, "input error: %v\n", ev.Err)
		return
	case terminal.EventKey:
	default:
		return
	}
	if ev.Key == terminal.KeyNone {
		return // ticker wake or unknown escape: render only
	}
	if ev.Key == terminal.KeyCtrlQ || ev.Key == terminal.KeyCtrlC {
		a.quit = true
		return
	}
	switch a.mode {
	case modeCommand:
		a.commandKey(ev)
	case modeEdit:
		a.editKey(ev)
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
	case ev.Key == terminal.KeyLeft || ev.Key == terminal.KeyRight || r == 'h' || r == 'l':
		a.kind ^= 1
	case ev.Key == terminal.KeyEnter:
		if a.selName() != "" {
			a.focus = focusInspector
		}
	case ev.Key == terminal.KeySpace || r == ' ':
		a.audition()
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

func (a *tuiApp) audition() {
	n := a.selName()
	if n == "" {
		return
	}
	if !addressable(n) {
		fmt.Fprintf(a.log, "%q: not addressable (space/dot/# in name)\n", n)
		return
	}
	if a.kind == kindSound {
		a.exec("play " + n)
		return
	}
	fmt.Fprintln(a.log, "patterns audition via a slot: press 0, 1 or 2")
}

func (a *tuiApp) assignSlot(slot int) {
	if a.kind != kindPattern {
		return
	}
	n := a.selName()
	if n == "" || !addressable(n) {
		return
	}
	a.exec(fmt.Sprintf("slot %d %s", slot, n))
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
	case ev.Key == terminal.KeyLeft || r == 'h':
		if m != nil {
			a.exp.Collapse(full)
		}
	case ev.Key == terminal.KeyEnter:
		a.inspectorEnter(m, full)
	case r == 'a':
		a.inspectorAdd(m)
	case r == 'x':
		if m != nil && m.elem {
			a.exec("del " + full)
		}
	case ev.Key == terminal.KeyEscape:
		a.focus = focusBrowser
	}
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
	a.exp.Expand(p) // show what was just appended
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
