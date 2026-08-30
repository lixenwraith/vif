package keys

import (
	"slices"
	"strings"

	"github.com/lixenwraith/terminal"
)

// Action is a UI verb. The table maps chords to actions; the app maps actions
// to functions. Adding a key is one table row.
type Action uint16

const (
	ActNone Action = iota
	ActQuit
	ActRedraw
	ActHelp
	ActCloseOverlay

	ActDown
	ActUp
	ActPageDown
	ActPageUp
	ActHalfDown
	ActHalfUp
	ActTop
	ActBottom

	ActColNext
	ActColPrev
	ActSort
	ActDetailDown
	ActDetailUp

	ActSearch
	ActFollowNext
	ActFollowPrev
	ActFilter // add/replace a stack filter
	ActClear

	ActExpand
	ActSnapNext
	ActSnapPrev

	ActPinToggle
	ActPinOnly
	ActPinClear

	ActOpen
	ActExport

	ActMark    // overlay: toggle multi-selection
	ActConfirm // overlay: activate the selected row
	ActBack    // overlay: leave the current level

	ActLvlTrace
	ActLvlDebug
	ActLvlInfo
	ActLvlWarn
	ActLvlError
	ActLvlProc
	ActLvlBad
	ActLvlAll
	ActLvlRaise
	ActLvlLower
	ActThresh1
	ActThresh2
	ActThresh3
	ActThresh4
	ActThresh5
)

// Mode scopes a binding to an input context.
type Mode uint8

const (
	ModeNormal Mode = iota
	ModeOverlay
)

// Chord is one key press.
type Chord struct {
	Key  terminal.Key
	Rune rune
	Mod  terminal.Modifier
}

// R builds a printable-rune chord.
func R(r rune) Chord { return Chord{Key: terminal.KeyRune, Rune: r} }

// K builds a named-key chord.
func K(k terminal.Key) Chord { return Chord{Key: k} }

// Binding is one row of the single key table that also generates the help overlay.
type Binding struct {
	Mode  Mode
	Seq   []Chord
	Act   Action
	Group string
	Help  string
}

// Bindings is the sole source of truth for keys, help text and footer hints.
// An empty Help hides the row from the overlay without unbinding the key: the
// level letters and the threshold digits are documented as one row each.
var Bindings = []Binding{
	{ModeNormal, []Chord{R('q')}, ActQuit, "general", "quit"},
	{ModeNormal, []Chord{K(terminal.KeyCtrlC)}, ActQuit, "general", "quit"},
	{ModeNormal, []Chord{K(terminal.KeyCtrlL)}, ActRedraw, "general", "redraw"},
	{ModeNormal, []Chord{R('?')}, ActHelp, "general", "help"},

	{ModeNormal, []Chord{R('j')}, ActDown, "move", "down"},
	{ModeNormal, []Chord{K(terminal.KeyDown)}, ActDown, "move", "down"},
	{ModeNormal, []Chord{R('k')}, ActUp, "move", "up"},
	{ModeNormal, []Chord{K(terminal.KeyUp)}, ActUp, "move", "up"},
	{ModeNormal, []Chord{K(terminal.KeyPageDown)}, ActPageDown, "move", "page down"},
	{ModeNormal, []Chord{K(terminal.KeyPageUp)}, ActPageUp, "move", "page up"},
	{ModeNormal, []Chord{K(terminal.KeyCtrlD)}, ActHalfDown, "move", "half page down"},
	{ModeNormal, []Chord{K(terminal.KeyCtrlU)}, ActHalfUp, "move", "half page up"},
	{ModeNormal, []Chord{R('g'), R('g')}, ActTop, "move", "first record"},
	{ModeNormal, []Chord{K(terminal.KeyHome)}, ActTop, "move", "first record"},
	{ModeNormal, []Chord{R('G')}, ActBottom, "move", "last record"},
	{ModeNormal, []Chord{K(terminal.KeyEnd)}, ActBottom, "move", "last record"},

	{ModeNormal, []Chord{K(terminal.KeyTab)}, ActColNext, "column", "focus next column"},
	{ModeNormal, []Chord{K(terminal.KeyBacktab)}, ActColPrev, "column", "focus previous column"},
	{ModeNormal, []Chord{R('s')}, ActSort, "column", "sort focused column: asc / desc / off"},
	{ModeNormal, []Chord{R('J')}, ActDetailDown, "column", "scroll detail down"},
	{ModeNormal, []Chord{R('K')}, ActDetailUp, "column", "scroll detail up"},

	{ModeNormal, []Chord{R('/')}, ActSearch, "search", "search the focused column"},
	{ModeNormal, []Chord{K(terminal.KeyEscape)}, ActClear, "search", "clear search and filters"},
	{ModeNormal, []Chord{R('f')}, ActFollowNext, "search", "next record like this one"},
	{ModeNormal, []Chord{R('F')}, ActFollowPrev, "search", "previous record like this one"},
	{ModeNormal, []Chord{R('\\')}, ActFilter, "search", "filter: kind:regexp (sub msg tick run level fields)"},

	{ModeNormal, []Chord{K(terminal.KeyEnter)}, ActExpand, "snapshot", "expand/collapse this snapshot"},
	{ModeNormal, []Chord{R('n')}, ActSnapNext, "snapshot", "jump to next snapshot down"},
	{ModeNormal, []Chord{R('N')}, ActSnapPrev, "snapshot", "jump to previous snapshot up"},

	{ModeNormal, []Chord{R(' ')}, ActPinToggle, "pin", "pin/unpin record"},
	{ModeNormal, []Chord{R('P')}, ActPinOnly, "pin", "show only pinned records"},
	{ModeNormal, []Chord{R('C')}, ActPinClear, "pin", "clear all pins"},

	{ModeNormal, []Chord{R('o')}, ActOpen, "file", "open a log file"},
	{ModeNormal, []Chord{R('x')}, ActExport, "file", "export pins, or the current result"},

	{ModeNormal, []Chord{R('t')}, ActLvlTrace, "level", "toggle one level: t d i w e p b"},
	{ModeNormal, []Chord{R('d')}, ActLvlDebug, "level", ""},
	{ModeNormal, []Chord{R('i')}, ActLvlInfo, "level", ""},
	{ModeNormal, []Chord{R('w')}, ActLvlWarn, "level", ""},
	{ModeNormal, []Chord{R('e')}, ActLvlError, "level", ""},
	{ModeNormal, []Chord{R('p')}, ActLvlProc, "level", ""},
	{ModeNormal, []Chord{R('b')}, ActLvlBad, "level", ""},
	{ModeNormal, []Chord{R('1')}, ActThresh1, "level", "hide below level: 1=TRACE … 5=ERROR"},
	{ModeNormal, []Chord{R('2')}, ActThresh2, "level", ""},
	{ModeNormal, []Chord{R('3')}, ActThresh3, "level", ""},
	{ModeNormal, []Chord{R('4')}, ActThresh4, "level", ""},
	{ModeNormal, []Chord{R('5')}, ActThresh5, "level", ""},
	{ModeNormal, []Chord{R('6')}, ActLvlProc, "level", ""},
	{ModeNormal, []Chord{R('7')}, ActLvlBad, "level", ""},
	{ModeNormal, []Chord{R('0')}, ActLvlAll, "level", "show all levels"},
	{ModeNormal, []Chord{R('>')}, ActLvlRaise, "level", "raise threshold"},
	{ModeNormal, []Chord{R('<')}, ActLvlLower, "level", "lower threshold"},

	{ModeOverlay, []Chord{K(terminal.KeyEscape)}, ActCloseOverlay, "overlay", "close"},
	{ModeOverlay, []Chord{R('q')}, ActCloseOverlay, "overlay", "close"},
	{ModeOverlay, []Chord{R('?')}, ActCloseOverlay, "overlay", ""},
	{ModeOverlay, []Chord{R('j')}, ActDown, "overlay", "down"},
	{ModeOverlay, []Chord{K(terminal.KeyDown)}, ActDown, "overlay", "down"},
	{ModeOverlay, []Chord{R('k')}, ActUp, "overlay", "up"},
	{ModeOverlay, []Chord{K(terminal.KeyUp)}, ActUp, "overlay", "up"},
	{ModeOverlay, []Chord{K(terminal.KeyPageDown)}, ActPageDown, "overlay", "page down"},
	{ModeOverlay, []Chord{K(terminal.KeyPageUp)}, ActPageUp, "overlay", "page up"},
	{ModeOverlay, []Chord{K(terminal.KeyCtrlD)}, ActHalfDown, "overlay", ""},
	{ModeOverlay, []Chord{K(terminal.KeyCtrlU)}, ActHalfUp, "overlay", ""},
	{ModeOverlay, []Chord{R('g'), R('g')}, ActTop, "overlay", "first row"},
	{ModeOverlay, []Chord{K(terminal.KeyHome)}, ActTop, "overlay", ""},
	{ModeOverlay, []Chord{R('G')}, ActBottom, "overlay", "last row"},
	{ModeOverlay, []Chord{K(terminal.KeyEnd)}, ActBottom, "overlay", ""},
	{ModeOverlay, []Chord{K(terminal.KeyEnter)}, ActConfirm, "overlay", "open file / enter directory"},
	{ModeOverlay, []Chord{R('l')}, ActConfirm, "overlay", ""},
	{ModeOverlay, []Chord{K(terminal.KeyRight)}, ActConfirm, "overlay", ""},
	{ModeOverlay, []Chord{R('h')}, ActBack, "overlay", "parent directory"},
	{ModeOverlay, []Chord{K(terminal.KeyLeft)}, ActBack, "overlay", ""},
	{ModeOverlay, []Chord{K(terminal.KeyBackspace)}, ActBack, "overlay", ""},
	{ModeOverlay, []Chord{R(' ')}, ActMark, "overlay", "mark file for multi-open"},
}

type skey struct {
	m Mode
	c Chord
}

type qkey struct {
	m    Mode
	a, b Chord
}

// Resolver turns chords into actions, holding one pending prefix for two-chord
// sequences.
type Resolver struct {
	single  map[skey]Action
	prefix  map[skey]bool
	seq     map[qkey]Action
	pending Chord
	armed   bool
}

// NewResolver compiles the binding table.
func NewResolver() *Resolver {
	r := &Resolver{
		single: make(map[skey]Action, len(Bindings)),
		prefix: make(map[skey]bool),
		seq:    make(map[qkey]Action),
	}
	for _, b := range Bindings {
		switch len(b.Seq) {
		case 1:
			r.single[skey{b.Mode, b.Seq[0]}] = b.Act
		case 2:
			r.prefix[skey{b.Mode, b.Seq[0]}] = true
			r.seq[qkey{b.Mode, b.Seq[0], b.Seq[1]}] = b.Act
		}
	}
	return r
}

// Resolve maps a chord to an action, returning ActNone while a prefix pends.
func (r *Resolver) Resolve(m Mode, c Chord) Action {
	if r.armed {
		r.armed = false
		if a, ok := r.seq[qkey{m, r.pending, c}]; ok {
			return a
		}
	}
	if r.prefix[skey{m, c}] {
		r.pending, r.armed = c, true
		return ActNone
	}
	return r.single[skey{m, c}]
}

// Reset drops any pending prefix.
func (r *Resolver) Reset() { r.armed = false }

// FromEvent builds a chord from a key event. Shift is dropped where the key
// identity already encodes it: runes carry their shifted form, and backtab is
// shift+tab by definition.
func FromEvent(ev terminal.Event) Chord {
	c := Chord{Key: ev.Key, Mod: ev.Modifiers}
	switch ev.Key {
	case terminal.KeyRune:
		c.Rune = ev.Rune
		c.Mod &^= terminal.ModShift
	case terminal.KeyBacktab:
		c.Mod &^= terminal.ModShift
	}
	return c
}

// ChordString renders a chord for help and hints.
func ChordString(c Chord) string {
	var b strings.Builder
	if c.Mod&terminal.ModCtrl != 0 {
		b.WriteByte('^')
	}
	if c.Mod&terminal.ModAlt != 0 {
		b.WriteString("M-")
	}
	if c.Key == terminal.KeyRune {
		if c.Rune == ' ' {
			b.WriteString("spc")
		} else {
			b.WriteRune(c.Rune)
		}
		return b.String()
	}
	if n := terminal.KeyName(c.Key); n != "" {
		b.WriteString(n)
		return b.String()
	}
	b.WriteByte('?')
	return b.String()
}

// SeqString renders a chord sequence.
func SeqString(s []Chord) string {
	var b strings.Builder
	for _, c := range s {
		b.WriteString(ChordString(c))
	}
	return b.String()
}

// KeysFor returns the chords bound to act, joined, for footer hints.
func KeysFor(m Mode, act Action) string {
	var out []string
	for _, b := range Bindings {
		if b.Mode == m && b.Act == act {
			out = append(out, SeqString(b.Seq))
		}
	}
	return strings.Join(out, "/")
}

// HelpRow is one generated line of the help overlay.
type HelpRow struct {
	Group string
	Keys  string
	Help  string
}

// HelpRows generates the help overlay content from the binding table, one row
// per action with its chords merged.
func HelpRows(m Mode) []HelpRow {
	type acc struct {
		keys        []string
		help, group string
	}
	byAct := map[Action]*acc{}
	var order []Action
	var groups []string

	for _, b := range Bindings {
		if b.Mode != m || b.Help == "" {
			continue
		}
		a, ok := byAct[b.Act]
		if !ok {
			a = &acc{help: b.Help, group: b.Group}
			byAct[b.Act] = a
			order = append(order, b.Act)
			if !slices.Contains(groups, b.Group) {
				groups = append(groups, b.Group)
			}
		}
		a.keys = append(a.keys, SeqString(b.Seq))
	}

	out := make([]HelpRow, 0, len(order))
	for _, g := range groups {
		for _, act := range order {
			if a := byAct[act]; a.group == g {
				out = append(out, HelpRow{Group: g, Keys: strings.Join(a.keys, " "), Help: a.help})
			}
		}
	}
	return out
}
