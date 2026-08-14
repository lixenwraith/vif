package input

import (
	"slices"
	"strconv"
	"sync"

	"github.com/lixenwraith/terminal"
)

// KeySection identifies one binding scope within a KeyTable
type KeySection uint8

const (
	SectionNormal   KeySection = iota // Normal and Visual mode
	SectionOperator                   // Motions accepted after an operator
	SectionPrefixG                    // Keys following the g prefix
	SectionOverlay                    // Overlay mode
	SectionText                       // Insert, Search and Command modes
)

// entryActions inverts actionRegistry. Built lazily so it does not depend on
// init order within the package.
var entryActions = sync.OnceValue(buildEntryActions)

// buildEntryActions maps each KeyEntry back to its canonical action name.
// Names are visited in sorted order, so a collision resolves deterministically;
// TestActionEntriesUnique asserts none exists.
func buildEntryActions() map[KeyEntry]string {
	names := make([]string, 0, len(actionRegistry))
	for name := range actionRegistry {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make(map[KeyEntry]string, len(names))
	for _, name := range names {
		entry := actionRegistry[name]
		if entry.Behavior == BehaviorNone {
			continue // The unbind sentinel is not an action
		}
		if _, dup := out[entry]; !dup {
			out[entry] = name
		}
	}
	return out
}

// EntryAction resolves a KeyEntry to its canonical action name
func EntryAction(entry KeyEntry) (string, bool) {
	name, ok := entryActions()[entry]
	return name, ok
}

// sectionMaps returns the rune and special-key maps backing a section
func (kt *KeyTable) sectionMaps(s KeySection) (map[rune]KeyEntry, map[terminal.Key]KeyEntry) {
	switch s {
	case SectionNormal:
		return kt.NormalRunes, kt.SpecialKeys
	case SectionOperator:
		return kt.OperatorMotions, nil
	case SectionPrefixG:
		return kt.PrefixG, nil
	case SectionOverlay:
		return kt.OverlayRunes, kt.OverlayKeys
	case SectionText:
		return nil, kt.TextNavKeys
	}
	return nil, nil
}

// Bindings returns the keys currently invoking an action in a section: rune
// bindings in code-point order first, then named keys alphabetically, so the
// first element is the primary display key. Empty when the action is unbound.
func (kt *KeyTable) Bindings(section KeySection, action string) []string {
	runes, keys := kt.sectionMaps(section)

	var rs []rune
	for r, e := range runes {
		if name, ok := EntryAction(e); ok && name == action {
			rs = append(rs, r)
		}
	}
	slices.Sort(rs)

	var ks []string
	for k, e := range keys {
		if name, ok := EntryAction(e); ok && name == action {
			ks = append(ks, SpecialKeyName(k))
		}
	}
	slices.Sort(ks)

	out := make([]string, 0, len(rs)+len(ks))
	for _, r := range rs {
		out = append(out, RuneKeyName(r))
	}
	return append(out, ks...)
}

// Actions returns every action bound in a section, sorted and deduplicated
func (kt *KeyTable) Actions(section KeySection) []string {
	runes, keys := kt.sectionMaps(section)

	seen := make(map[string]struct{}, len(runes)+len(keys))
	for _, e := range runes {
		if name, ok := EntryAction(e); ok {
			seen[name] = struct{}{}
		}
	}
	for _, e := range keys {
		if name, ok := EntryAction(e); ok {
			seen[name] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// runeNames overrides runes that render poorly bare
var runeNames = map[rune]string{
	' ': "Space",
}

// RuneKeyName returns the display form of a rune binding
func RuneKeyName(r rune) string {
	if s, ok := runeNames[r]; ok {
		return s
	}
	return string(r)
}

// specialNames is the display vocabulary for named keys; an unlisted key falls
// back to a numeric form so a new binding is visible rather than silent
var specialNames = map[terminal.Key]string{
	terminal.KeyUp:        "Up",
	terminal.KeyDown:      "Down",
	terminal.KeyLeft:      "Left",
	terminal.KeyRight:     "Right",
	terminal.KeyHome:      "Home",
	terminal.KeyEnd:       "End",
	terminal.KeyPageUp:    "PgUp",
	terminal.KeyPageDown:  "PgDn",
	terminal.KeyTab:       "Tab",
	terminal.KeyBacktab:   "Shift+Tab",
	terminal.KeyEnter:     "Enter",
	terminal.KeyBackspace: "Backspace",
	terminal.KeyDelete:    "Delete",
	terminal.KeyEscape:    "ESC",
	terminal.KeyCtrlA:     "Ctrl+A",
	terminal.KeyCtrlC:     "Ctrl+C",
	terminal.KeyCtrlE:     "Ctrl+E",
	terminal.KeyCtrlK:     "Ctrl+K",
	terminal.KeyCtrlQ:     "Ctrl+Q",
	terminal.KeyCtrlS:     "Ctrl+S",
	terminal.KeyCtrlU:     "Ctrl+U",
	terminal.KeyCtrlW:     "Ctrl+W",
	terminal.KeyCtrlSpace: "Ctrl+Space",
}

// SpecialKeyName returns the display form of a named-key binding
func SpecialKeyName(k terminal.Key) string {
	if s, ok := specialNames[k]; ok {
		return s
	}
	return "key#" + strconv.Itoa(int(k))
}
