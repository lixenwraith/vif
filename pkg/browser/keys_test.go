package browser

import (
	"testing"

	"github.com/lixenwraith/terminal"
)

// TestAKeyPressReportsWhatATerminalReports: a KeyboardEvent's key and modifiers
// become the event the terminal module parses from the bytes a terminal sends for
// that key; what a terminal never sees is refused.
func TestAKeyPressReportsWhatATerminalReports(t *testing.T) {
	const shift, alt, ctrl, meta = modShift, modAlt, modCtrl, modMeta
	key := func(k terminal.Key, m terminal.Modifier) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: k, Modifiers: m}
	}
	char := func(r rune, m terminal.Modifier) terminal.Event {
		return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r, Modifiers: m}
	}
	for _, c := range []struct {
		key  string
		mods uint8
		want terminal.Event
	}{
		{"a", 0, char('a', 0)},
		{"A", shift, char('A', 0)},
		{" ", 0, char(' ', 0)},
		{"x", alt, char('x', terminal.ModAlt)},
		{"λ", 0, char('λ', 0)},
		{"c", ctrl, key(terminal.KeyCtrlC, 0)},
		{"C", ctrl | shift, key(terminal.KeyCtrlC, 0)},
		{"h", ctrl, key(terminal.KeyBackspace, 0)},
		{"[", ctrl, key(terminal.KeyEscape, 0)},
		{" ", ctrl, key(terminal.KeyCtrlSpace, 0)},
		{"g", ctrl | alt, key(terminal.KeyCtrlG, terminal.ModAlt)},
		{"Enter", 0, key(terminal.KeyEnter, 0)},
		{"Enter", ctrl, key(terminal.KeyEnter, 0)},
		{"Tab", 0, key(terminal.KeyTab, 0)},
		{"Tab", shift, key(terminal.KeyBacktab, terminal.ModShift)},
		{"Backspace", 0, key(terminal.KeyBackspace, 0)},
		{"Delete", ctrl, key(terminal.KeyDelete, terminal.ModCtrl)},
		{"ArrowUp", shift, key(terminal.KeyUp, terminal.ModShift)},
		{"F5", alt, key(terminal.KeyF5, terminal.ModAlt)},
	} {
		if got, ok := keyEvent(c.key, c.mods); !ok || got != c.want {
			t.Errorf("%q with %03b: %+v %v, want %+v", c.key, c.mods, got, ok, c.want)
		}
	}
	for _, c := range []struct {
		key  string
		mods uint8
	}{
		{"Shift", shift}, {"Dead", 0}, {"Unidentified", 0}, {"Process", 0},
		{"v", meta}, {"ArrowUp", meta}, {"1", ctrl}, {"", 0},
	} {
		if ev, ok := keyEvent(c.key, c.mods); ok {
			t.Errorf("%q with %03b was taken as %+v", c.key, c.mods, ev)
		}
	}
	if ev := textEvent('\n'); ev.Key != terminal.KeyEnter {
		t.Errorf("a newline typed %+v, want Enter", ev)
	}
}

// TestThePointerIsReportedInTheTerminalsNumbering: button and action as the page
// numbers them are the terminal's, with the modifiers; a number outside either is
// refused.
func TestThePointerIsReportedInTheTerminalsNumbering(t *testing.T) {
	want := terminal.Event{Type: terminal.EventMouse, MouseX: 3, MouseY: 7,
		MouseBtn: terminal.MouseBtnRight, MouseAction: terminal.MouseActionPress, Modifiers: terminal.ModCtrl}
	if got, ok := mouseEvent(3, 7, 3, 1, modCtrl); !ok || got != want {
		t.Errorf("right press: %+v %v, want %+v", got, ok, want)
	}
	want = terminal.Event{Type: terminal.EventMouse, MouseX: 0, MouseY: 0,
		MouseBtn: terminal.MouseBtnWheelDown, MouseAction: terminal.MouseActionPress}
	if got, ok := mouseEvent(-1, -2, 5, 1, 0); !ok || got != want {
		t.Errorf("wheel: %+v %v, want %+v", got, ok, want)
	}
	for _, c := range [][2]int{{0, 1}, {8, 1}, {1, 0}, {1, 5}} {
		if ev, ok := mouseEvent(0, 0, c[0], c[1], 0); ok {
			t.Errorf("button %d action %d was taken as %+v", c[0], c[1], ev)
		}
	}
}
