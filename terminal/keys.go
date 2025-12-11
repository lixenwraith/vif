// @focus: #sys { io } #input { keys }
package terminal

// Key represents a parsed input key
type Key uint16

// Key constants - designed for expansion
const (
	KeyNone Key = iota
	KeyRune     // Printable character (check Event.Rune)

	// Control keys
	KeyEscape
	KeyEnter
	KeyTab
	KeyBacktab // Shift+Tab
	KeyBackspace
	KeyDelete
	KeySpace

	// Navigation
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert

	// Function keys
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12

	// Ctrl+letter (Ctrl+A = 0x01, Ctrl+Z = 0x1A)
	KeyCtrlA
	KeyCtrlB
	KeyCtrlC
	KeyCtrlD
	KeyCtrlE
	KeyCtrlF
	KeyCtrlG
	KeyCtrlH // Often same as Backspace
	KeyCtrlI // Often same as Tab
	KeyCtrlJ // Often same as Enter
	KeyCtrlK
	KeyCtrlL
	KeyCtrlM // Often same as Enter
	KeyCtrlN
	KeyCtrlO
	KeyCtrlP
	KeyCtrlQ
	KeyCtrlR
	KeyCtrlS
	KeyCtrlT
	KeyCtrlU
	KeyCtrlV
	KeyCtrlW
	KeyCtrlX
	KeyCtrlY
	KeyCtrlZ

	// Ctrl+special
	KeyCtrlSpace
	KeyCtrlBackslash
	KeyCtrlBracketLeft
	KeyCtrlBracketRight
	KeyCtrlCaret
	KeyCtrlUnderscore
)

// Modifier flags
type Modifier uint8

const (
	ModNone  Modifier = 0
	ModShift Modifier = 1 << 0
	ModAlt   Modifier = 1 << 1
	ModCtrl  Modifier = 1 << 2
)

// escapeSequence maps escape sequences to keys
// Key: sequence after ESC [ (e.g., "A" for up arrow)
type escapeSequence struct {
	seq string
	key Key
	mod Modifier
}

// Known escape sequences (CSI sequences: ESC [ ...)
var csiSequences = []escapeSequence{
	// Arrow keys
	{"A", KeyUp, ModNone},
	{"B", KeyDown, ModNone},
	{"C", KeyRight, ModNone},
	{"D", KeyLeft, ModNone},
	{"Z", KeyBacktab, ModShift}, // Shift+Tab

	// Arrow keys with modifiers (xterm style: ESC [ 1 ; mod X)
	{"1;2A", KeyUp, ModShift},
	{"1;2B", KeyDown, ModShift},
	{"1;2C", KeyRight, ModShift},
	{"1;2D", KeyLeft, ModShift},
	{"1;3A", KeyUp, ModAlt},
	{"1;3B", KeyDown, ModAlt},
	{"1;3C", KeyRight, ModAlt},
	{"1;3D", KeyLeft, ModAlt},
	{"1;5A", KeyUp, ModCtrl},
	{"1;5B", KeyDown, ModCtrl},
	{"1;5C", KeyRight, ModCtrl},
	{"1;5D", KeyLeft, ModCtrl},

	// Navigation
	{"H", KeyHome, ModNone},
	{"F", KeyEnd, ModNone},
	{"1~", KeyHome, ModNone},
	{"4~", KeyEnd, ModNone},
	{"5~", KeyPageUp, ModNone},
	{"6~", KeyPageDown, ModNone},
	{"2~", KeyInsert, ModNone},
	{"3~", KeyDelete, ModNone},

	// Function keys (xterm)
	{"11~", KeyF1, ModNone},
	{"12~", KeyF2, ModNone},
	{"13~", KeyF3, ModNone},
	{"14~", KeyF4, ModNone},
	{"15~", KeyF5, ModNone},
	{"17~", KeyF6, ModNone},
	{"18~", KeyF7, ModNone},
	{"19~", KeyF8, ModNone},
	{"20~", KeyF9, ModNone},
	{"21~", KeyF10, ModNone},
	{"23~", KeyF11, ModNone},
	{"24~", KeyF12, ModNone},

	// Function keys (vt style)
	{"[A", KeyF1, ModNone},
	{"[B", KeyF2, ModNone},
	{"[C", KeyF3, ModNone},
	{"[D", KeyF4, ModNone},
	{"[E", KeyF5, ModNone},
}

// SS3 sequences (ESC O ...)
var ss3Sequences = []escapeSequence{
	{"A", KeyUp, ModNone},
	{"B", KeyDown, ModNone},
	{"C", KeyRight, ModNone},
	{"D", KeyLeft, ModNone},
	{"H", KeyHome, ModNone},
	{"F", KeyEnd, ModNone},
	{"P", KeyF1, ModNone},
	{"Q", KeyF2, ModNone},
	{"R", KeyF3, ModNone},
	{"S", KeyF4, ModNone},
}

var csiMap = buildSequenceMap(csiSequences)
var ss3Map = buildSequenceMap(ss3Sequences)

func buildSequenceMap(seqs []escapeSequence) map[string]escapeSequence {
	m := make(map[string]escapeSequence, len(seqs))
	for _, s := range seqs {
		m[s.seq] = s
	}
	return m
}

// lookupCSI performs zero-alloc map lookup via compiler optimization
// The string([]byte) conversion inline in map access does not allocate
func lookupCSI(seq []byte) (Key, Modifier, bool) {
	if s, ok := csiMap[string(seq)]; ok {
		return s.key, s.mod, true
	}
	return KeyNone, ModNone, false
}

// lookupSS3 performs zero-alloc map lookup
func lookupSS3(seq []byte) (Key, Modifier, bool) {
	if s, ok := ss3Map[string(seq)]; ok {
		return s.key, s.mod, true
	}
	return KeyNone, ModNone, false
}