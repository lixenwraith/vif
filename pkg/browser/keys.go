package browser

import (
	"unicode"
	"unicode/utf8"

	"github.com/lixenwraith/terminal"
)

// Modifier bits as the page reports them on a key or a pointer event
const (
	modShift = 1 << 0
	modAlt   = 1 << 1
	modCtrl  = 1 << 2
	modMeta  = 1 << 3
)

// namedKeys maps KeyboardEvent.key values of non-printing keys to the key a terminal
// reports for them
var namedKeys = map[string]terminal.Key{
	"Escape": terminal.KeyEscape, "Enter": terminal.KeyEnter, "Tab": terminal.KeyTab,
	"Backspace": terminal.KeyBackspace, "Delete": terminal.KeyDelete,
	"ArrowUp": terminal.KeyUp, "ArrowDown": terminal.KeyDown,
	"ArrowLeft": terminal.KeyLeft, "ArrowRight": terminal.KeyRight,
	"Home": terminal.KeyHome, "End": terminal.KeyEnd,
	"PageUp": terminal.KeyPageUp, "PageDown": terminal.KeyPageDown, "Insert": terminal.KeyInsert,
	"F1": terminal.KeyF1, "F2": terminal.KeyF2, "F3": terminal.KeyF3, "F4": terminal.KeyF4,
	"F5": terminal.KeyF5, "F6": terminal.KeyF6, "F7": terminal.KeyF7, "F8": terminal.KeyF8,
	"F9": terminal.KeyF9, "F10": terminal.KeyF10, "F11": terminal.KeyF11, "F12": terminal.KeyF12,
}

// keyEvent is the terminal event for a key press: key as the browser resolved it with
// Shift applied, mods the modifier bits. ok is false for a key a terminal never sees:
// a modifier alone, a dead key, a Meta chord, or the "Unidentified" a soft keyboard
// sends ahead of its text.
func keyEvent(key string, mods uint8) (terminal.Event, bool) {
	ev := terminal.Event{Type: terminal.EventKey}
	if mods&modMeta != 0 {
		return ev, false
	}
	if k, ok := namedKeys[key]; ok {
		ev.Key = k
		if k == terminal.KeyTab && mods&modShift != 0 {
			ev.Key, ev.Modifiers = terminal.KeyBacktab, terminal.ModShift
			return ev, true
		}
		// A terminal reports modifiers on the editing, navigation and function keys only
		if k == terminal.KeyDelete || k >= terminal.KeyUp && k <= terminal.KeyF12 {
			ev.Modifiers = modifiers(mods)
		}
		return ev, true
	}
	r, size := utf8.DecodeRuneInString(key)
	if size == 0 || size != len(key) || r == utf8.RuneError {
		return ev, false
	}
	if mods&modCtrl != 0 {
		k, ok := terminal.ControlKey(unicode.ToLower(r))
		if !ok {
			return ev, false
		}
		ev.Key = k
	} else {
		ev.Key, ev.Rune = terminal.KeyRune, r
	}
	if mods&modAlt != 0 {
		ev.Modifiers = terminal.ModAlt
	}
	return ev, true
}

// textEvent is one character the page's text field committed, a soft keyboard's
// or a paste's; a newline presses Enter
func textEvent(r rune) terminal.Event {
	if r == '\n' {
		return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter}
	}
	return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}
}

// mouseEvent is the terminal event for the pointer at cell x, y: button and action in
// the terminal's own numbering, which the page uses; ok is false outside it
func mouseEvent(x, y, button, action int, mods uint8) (terminal.Event, bool) {
	if button < int(terminal.MouseBtnLeft) || button > int(terminal.MouseBtnForward) ||
		action < int(terminal.MouseActionPress) || action > int(terminal.MouseActionDrag) {
		return terminal.Event{}, false
	}
	return terminal.Event{Type: terminal.EventMouse, MouseX: max(x, 0), MouseY: max(y, 0),
		MouseBtn: terminal.MouseButton(button), MouseAction: terminal.MouseAction(action),
		Modifiers: modifiers(mods)}, true
}

func modifiers(mods uint8) terminal.Modifier {
	var m terminal.Modifier
	if mods&modShift != 0 {
		m |= terminal.ModShift
	}
	if mods&modAlt != 0 {
		m |= terminal.ModAlt
	}
	if mods&modCtrl != 0 {
		m |= terminal.ModCtrl
	}
	return m
}
