package mode

import (
	"strings"
	"testing"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// consoleRun is a router over an 80x24 run and its keys and pointer
type consoleRun struct {
	t       *testing.T
	ctx     *engine.GameContext
	machine *input.Machine
	r       *Router
}

func newConsoleRun(t *testing.T) *consoleRun {
	ctx := engine.NewGameContext(engine.NewWorld(), 80, 24)
	machine := input.NewMachine()
	return &consoleRun{t: t, ctx: ctx, machine: machine, r: NewRouter(ctx, machine)}
}

func (c *consoleRun) key(k terminal.Key, ch rune) {
	c.t.Helper()
	if intent := c.machine.Process(terminal.Event{Type: terminal.EventKey, Key: k, Rune: ch}); intent != nil {
		c.r.Handle(intent)
	}
}

func (c *consoleRun) typed(s string) {
	c.t.Helper()
	for _, ch := range s {
		c.key(terminal.KeyRune, ch)
	}
}

func (c *consoleRun) mouse(btn terminal.MouseButton, action terminal.MouseAction, x, y int) {
	c.t.Helper()
	ev := terminal.Event{Type: terminal.EventMouse, MouseBtn: btn, MouseAction: action, MouseX: x, MouseY: y}
	if intent := c.machine.Process(ev); intent != nil {
		c.r.Handle(intent)
	}
}

// last is the console's newest lines, n of them
func (c *consoleRun) last(n int) []engine.ConsoleLine {
	lines, _ := c.ctx.ConsoleLines(nil)
	return lines[max(len(lines)-n, 0):]
}

// TestTheBacktickOpensTheConsoleAndItsCommandLineReturnsToIt: the backtick opens the
// console at its newest row and closes it. : in it opens the command line under it;
// Enter runs the command and gives the keys back to the console, at its foot, where
// the command and its answer are listed by kind; Escape gives them back without
// running it. A press outside the console closes it, and the overlay with it.
func TestTheBacktickOpensTheConsoleAndItsCommandLineReturnsToIt(t *testing.T) {
	c := newConsoleRun(t)
	ctx := c.ctx

	c.key(terminal.KeyRune, '`')
	if !ctx.IsConsoleShown() || !ctx.IsOverlayMode() || ctx.GetOverlayScroll() != parameter.ConsoleFollow {
		t.Fatalf("the backtick: console %v, mode %v, scroll %d", ctx.IsConsoleShown(), ctx.GetMode(), ctx.GetOverlayScroll())
	}
	c.key(terminal.KeyRune, 'k') // off the foot, so returning shows it again
	c.key(terminal.KeyRune, ':')
	if !ctx.IsCommandMode() || !ctx.IsConsoleShown() {
		t.Fatalf(": in the console: mode %v, console %v", ctx.GetMode(), ctx.IsConsoleShown())
	}
	c.typed("frobnicate")
	c.key(terminal.KeyEnter, 0)
	if !ctx.IsOverlayMode() || !ctx.IsConsoleShown() || ctx.GetOverlayScroll() != parameter.ConsoleFollow {
		t.Fatalf("Enter: mode %v, console %v, scroll %d", ctx.GetMode(), ctx.IsConsoleShown(), ctx.GetOverlayScroll())
	}
	if got := c.last(2); got[0].Text != ":frobnicate" || got[0].Kind != parameter.StatusInput ||
		!strings.Contains(got[1].Text, "frobnicate") || got[1].Kind != parameter.StatusError {
		t.Fatalf("the console ends %+v, want the command as input and its refusal as an error", got)
	}

	scale := ctx.TimeCtl.Scale()
	c.key(terminal.KeyRune, ':')
	c.typed("speed 2")
	c.key(terminal.KeyEscape, 0)
	if !ctx.IsOverlayMode() || !ctx.IsConsoleShown() || ctx.GetCommandText() != "" || ctx.TimeCtl.Scale() != scale {
		t.Fatalf("Escape: mode %v, console %v, line %q, scale %v", ctx.GetMode(), ctx.IsConsoleShown(), ctx.GetCommandText(), ctx.TimeCtl.Scale())
	}
	c.key(terminal.KeyRune, 'i') // a mode an overlay opens none of
	if !ctx.IsOverlayMode() {
		t.Fatalf("i in the console entered %v", ctx.GetMode())
	}

	c.key(terminal.KeyRune, '`')
	if ctx.IsOverlayActive() || !ctx.IsNormalMode() {
		t.Fatalf("the backtick again: overlay %v, mode %v", ctx.IsOverlayActive(), ctx.GetMode())
	}

	c.key(terminal.KeyRune, '`')
	g := ctx.OverlayGeometry()
	c.mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, g.X+2, g.Y-1)
	if ctx.IsOverlayActive() || !ctx.IsNormalMode() {
		t.Fatalf("a press on the map above the console: overlay %v, mode %v", ctx.IsOverlayActive(), ctx.GetMode())
	}
}

// TestTheConsoleFollowsItsFootUntilScrolledOff: the console keeps to its newest row
// until a key, the wheel or the scrollbar moves it off, and follows again once one
// brings it back. Under the command line, PgUp and PgDn page it, the wheel and the
// scrollbar move it, and a press elsewhere leaves the line as typed.
func TestTheConsoleFollowsItsFootUntilScrolledOff(t *testing.T) {
	c := newConsoleRun(t)
	ctx := c.ctx
	c.key(terminal.KeyRune, '`')
	g := ctx.OverlayGeometry()
	total := 10 * g.ContentH
	ctx.SetOverlayContentH(total) // as the renderer lays the rows out
	foot := total - g.ContentH
	page := max(g.ContentH-parameter.OverlayPageOverlap, 1)

	c.key(terminal.KeyRune, 'k')
	if got := ctx.GetOverlayScroll(); got != foot-1 {
		t.Fatalf("k from the foot scrolled to %d, want %d", got, foot-1)
	}
	c.key(terminal.KeyRune, 'j')
	if got := ctx.GetOverlayScroll(); got != parameter.ConsoleFollow {
		t.Fatalf("j back to the foot left the view at %d, not following", got)
	}
	c.key(terminal.KeyRune, 'g')
	c.key(terminal.KeyPageDown, 0)
	if got := ctx.GetOverlayScroll(); got != page {
		t.Fatalf("g then PgDn scrolled to %d, want %d", got, page)
	}
	c.key(terminal.KeyRune, 'G')
	if got := ctx.GetOverlayScroll(); got != parameter.ConsoleFollow {
		t.Fatalf("G left the view at %d, not following", got)
	}

	c.key(terminal.KeyRune, ':')
	c.typed("speed")
	c.key(terminal.KeyPageUp, 0)
	if got := ctx.GetOverlayScroll(); got != foot-page {
		t.Fatalf("PgUp under the command line scrolled to %d, want %d", got, foot-page)
	}
	c.mouse(terminal.MouseBtnWheelDown, terminal.MouseActionPress, 0, 0)
	if got := ctx.GetOverlayScroll(); got != foot-page+parameter.OverlayWheelRows {
		t.Fatalf("the wheel under the command line scrolled to %d", got)
	}
	bar, top := g.X+g.ScrollX, g.Y+g.ContentY
	c.mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, bar, top)
	c.mouse(terminal.MouseBtnLeft, terminal.MouseActionRelease, bar, top)
	if got := ctx.GetOverlayScroll(); got != 0 {
		t.Fatalf("a press at the track's top under the command line scrolled to %d", got)
	}
	c.mouse(terminal.MouseBtnLeft, terminal.MouseActionPress, 2, 0)
	if !ctx.IsCommandMode() || ctx.GetCommandText() != "speed" || !ctx.IsConsoleShown() {
		t.Fatalf("a press above the console: mode %v, line %q", ctx.GetMode(), ctx.GetCommandText())
	}
	c.key(terminal.KeyPageDown, 0)
	c.key(terminal.KeyPageDown, 0)
	for range total / page {
		c.key(terminal.KeyPageDown, 0)
	}
	if got := ctx.GetOverlayScroll(); got != parameter.ConsoleFollow {
		t.Fatalf("paging down to the foot under the command line left the view at %d, not following", got)
	}
}

// TestTheConsoleCommandOpensAndClearsIt: :console opens the console as the backtick
// does, from the command line and from the menu's row, and :console clear empties it
func TestTheConsoleCommandOpensAndClearsIt(t *testing.T) {
	c := newConsoleRun(t)
	ctx := c.ctx
	c.key(terminal.KeyRune, ':')
	c.typed("console")
	c.key(terminal.KeyEnter, 0)
	if !ctx.IsConsoleShown() || !ctx.IsOverlayMode() || ctx.GetOverlayScroll() != parameter.ConsoleFollow {
		t.Fatalf(":console: console %v, mode %v, scroll %d", ctx.IsConsoleShown(), ctx.GetMode(), ctx.GetOverlayScroll())
	}
	c.key(terminal.KeyRune, ':')
	c.typed("console clear")
	c.key(terminal.KeyEnter, 0)
	if lines := c.last(1); len(lines) != 0 || !ctx.IsConsoleShown() {
		t.Fatalf(":console clear left %+v, console %v", lines, ctx.IsConsoleShown())
	}
	ExecuteCommand(ctx, "console twice")
	if got := c.last(1); len(got) != 1 || got[0].Kind != parameter.StatusError {
		t.Fatalf(":console with a stray word ends the console with %+v", got)
	}

	c.r.closeOverlay()
	c.r.handleConfigMenu()
	ctx.SetOverlaySelection("game")
	c.key(terminal.KeyEnter, 0)
	ctx.SetOverlaySelection("game/console")
	c.key(terminal.KeyEnter, 0)
	if !ctx.IsConsoleShown() || configMenu(ctx) != nil || !ctx.IsOverlayMode() {
		t.Fatalf("the menu's Console row: console %v, menu %v, mode %v", ctx.IsConsoleShown(), configMenu(ctx) != nil, ctx.GetMode())
	}
}

// TestTheEchoHidesEverySecretTyped: a command is echoed as typed but for the words
// that carry a passphrase or a password, which the console would otherwise keep
func TestTheEchoHidesEverySecretTyped(t *testing.T) {
	s := parameter.ConsoleSecret
	for typed, want := range map[string]string{
		"speed  2":                              "speed  2",
		"join host:7777":                        "join host:7777",
		"join host:7777 hunter2":                "join host:7777 " + s,
		"join host:7777 two words":              "join host:7777 " + s + " " + s,
		"host :7777 passphrase":                 "host :7777 passphrase",
		"host :7777 migrate passphrase hunter2": "host :7777 migrate passphrase " + s,
		"register https://x.test alice hunter2": "register https://x.test alice " + s,
		"login https://x.test alice hunter2":    "login https://x.test alice " + s,
		"login https://x.test alice":            "login https://x.test alice",
		"logout https://x.test":                 "logout https://x.test",
	} {
		if got := echoed(typed); got != want {
			t.Errorf("echoed(%q) = %q, want %q", typed, got, want)
		}
	}

	ctx := engine.NewGameContext(engine.NewWorld(), 80, 24)
	ExecuteCommand(ctx, "join host:7777 hunter2")
	ExecuteCommand(ctx, "login https://x.test alice hunter2")
	lines, _ := ctx.ConsoleLines(nil)
	for _, line := range lines {
		if strings.Contains(line.Text, "hunter2") {
			t.Fatalf("the console lists %q", line.Text)
		}
	}
	if got := ctx.GetStatusMessage(); strings.Contains(got, "hunter2") {
		t.Fatalf("the status line reads %q", got)
	}
}
