package game

import (
	"unicode"

	"github.com/lixenwraith/terminal"
)

// Android's KeyEvent meta state bits
const (
	metaShift = 0x1
	metaAlt   = 0x2
	metaCtrl  = 0x1000
)

// androidKeys maps Android key codes to the keys a terminal reports for them; Back
// is Escape, the key that leaves a mode
var androidKeys = map[int32]terminal.Key{
	4:   terminal.KeyEscape, // KEYCODE_BACK
	111: terminal.KeyEscape,
	66:  terminal.KeyEnter,
	160: terminal.KeyEnter, // numeric keypad
	61:  terminal.KeyTab,
	67:  terminal.KeyBackspace, // KEYCODE_DEL
	112: terminal.KeyDelete,    // KEYCODE_FORWARD_DEL
	19:  terminal.KeyUp,
	20:  terminal.KeyDown,
	21:  terminal.KeyLeft,
	22:  terminal.KeyRight,
	122: terminal.KeyHome,
	123: terminal.KeyEnd,
	92:  terminal.KeyPageUp,
	93:  terminal.KeyPageDown,
	124: terminal.KeyInsert,
	131: terminal.KeyF1, 132: terminal.KeyF2, 133: terminal.KeyF3, 134: terminal.KeyF4,
	135: terminal.KeyF5, 136: terminal.KeyF6, 137: terminal.KeyF7, 138: terminal.KeyF8,
	139: terminal.KeyF9, 140: terminal.KeyF10, 141: terminal.KeyF11, 142: terminal.KeyF12,
}

// keyEvent is the terminal event for an Android key: a named key, Ctrl with a
// letter as the control byte a terminal reads, or the character it types
func keyEvent(code int32, char rune, meta int32) (terminal.Event, bool) {
	var mods terminal.Modifier
	if meta&metaShift != 0 {
		mods |= terminal.ModShift
	}
	if meta&metaAlt != 0 {
		mods |= terminal.ModAlt
	}
	if key, ok := androidKeys[code]; ok {
		if key == terminal.KeyTab && mods&terminal.ModShift != 0 {
			key = terminal.KeyBacktab
		}
		if meta&metaCtrl != 0 {
			mods |= terminal.ModCtrl
		}
		return terminal.Event{Type: terminal.EventKey, Key: key, Modifiers: mods}, true
	}
	if char == 0 {
		return terminal.Event{}, false
	}
	if meta&metaCtrl != 0 {
		key, ok := ctrlKey(char)
		return terminal.Event{Type: terminal.EventKey, Key: key, Modifiers: mods &^ terminal.ModShift}, ok
	}
	return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: char, Modifiers: mods & terminal.ModAlt}, true
}

// ctrlKey is Ctrl with a character as a terminal parses its control byte: H, I, J
// and M are Backspace, Tab and Enter there
func ctrlKey(r rune) (terminal.Key, bool) {
	switch r = unicode.ToLower(r); {
	case r == 'h':
		return terminal.KeyBackspace, true
	case r == 'i':
		return terminal.KeyTab, true
	case r == 'j' || r == 'm':
		return terminal.KeyEnter, true
	case r >= 'a' && r <= 'z':
		return terminal.KeyCtrlA + terminal.Key(r-'a'), true
	case r == ' ' || r == '@':
		return terminal.KeyCtrlSpace, true
	case r == '[':
		return terminal.KeyEscape, true
	case r == '\\':
		return terminal.KeyCtrlBackslash, true
	case r == ']':
		return terminal.KeyCtrlBracketRight, true
	}
	return terminal.KeyNone, false
}

// textEvent is one character the system keyboard committed
func textEvent(r rune) terminal.Event {
	if r == '\n' {
		return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyEnter}
	}
	return terminal.Event{Type: terminal.EventKey, Key: terminal.KeyRune, Rune: r}
}

// touch is the left button pressed or released on the cell under pixel x, y. A
// press off the grid names no cell and is dropped; a release is kept on its edge,
// since it ends whatever press came before.
func (v *view) touch(x, y int, down bool) (terminal.Event, bool) {
	if !v.pointer.Load() {
		return terminal.Event{}, false
	}
	cols, rows := v.Size()
	cx, cy := x/v.font.atlas.Width, y/v.font.atlas.Height
	inside := x >= 0 && y >= 0 && cx < cols && cy < rows
	action := terminal.MouseActionRelease
	if down {
		if !inside {
			return terminal.Event{}, false
		}
		action = terminal.MouseActionPress
	}
	return terminal.Event{
		Type: terminal.EventMouse, MouseBtn: terminal.MouseBtnLeft, MouseAction: action,
		MouseX: min(max(cx, 0), cols-1), MouseY: min(max(cy, 0), rows-1),
	}, true
}
