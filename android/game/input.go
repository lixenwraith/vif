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

// ctrlKeys is what a terminal reports for Ctrl and a printable key, the control
// byte it receives parsed as the terminal module parses it; the desktop window
// keeps the same table
var ctrlKeys = map[rune]terminal.Key{
	' ': terminal.KeyCtrlSpace, '@': terminal.KeyCtrlSpace, '2': terminal.KeyCtrlSpace,
	'h': terminal.KeyBackspace, 'i': terminal.KeyTab, 'j': terminal.KeyEnter, 'm': terminal.KeyEnter,
	'[': terminal.KeyEscape, '3': terminal.KeyEscape,
	'\\': terminal.KeyCtrlBackslash, '4': terminal.KeyCtrlBackslash,
	']': terminal.KeyCtrlBracketRight, '5': terminal.KeyCtrlBracketRight,
	'^': terminal.KeyCtrlCaret, '6': terminal.KeyCtrlCaret,
	'_': terminal.KeyCtrlUnderscore, '-': terminal.KeyCtrlUnderscore, '/': terminal.KeyCtrlUnderscore, '7': terminal.KeyCtrlUnderscore,
}

// keyEvent is the terminal event for an Android key: a named key, Ctrl with a
// character as the control byte a terminal reads, or the character it types. As
// a terminal, it reports modifiers on the editing, navigation and function keys
// only, and Alt on a character.
func keyEvent(code int32, char rune, meta int32) (terminal.Event, bool) {
	ev := terminal.Event{Type: terminal.EventKey}
	if key, ok := androidKeys[code]; ok {
		ev.Key = key
		if key == terminal.KeyTab && meta&metaShift != 0 {
			ev.Key, ev.Modifiers = terminal.KeyBacktab, terminal.ModShift
		}
		if key == terminal.KeyDelete || key >= terminal.KeyUp && key <= terminal.KeyF12 {
			ev.Modifiers = modifiers(meta)
		}
		return ev, true
	}
	if char == 0 {
		return ev, false
	}
	if meta&metaCtrl != 0 {
		lower := unicode.ToLower(char)
		switch k, ok := ctrlKeys[lower]; {
		case ok:
			ev.Key = k
		case lower >= 'a' && lower <= 'z':
			ev.Key = terminal.KeyCtrlA + terminal.Key(lower-'a')
		default:
			return ev, false
		}
	} else {
		ev.Key, ev.Rune = terminal.KeyRune, char
	}
	ev.Modifiers = modifiers(meta) & terminal.ModAlt
	return ev, true
}

func modifiers(meta int32) terminal.Modifier {
	var m terminal.Modifier
	if meta&metaShift != 0 {
		m |= terminal.ModShift
	}
	if meta&metaAlt != 0 {
		m |= terminal.ModAlt
	}
	if meta&metaCtrl != 0 {
		m |= terminal.ModCtrl
	}
	return m
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
