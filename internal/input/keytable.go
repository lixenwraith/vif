package input

import (
	_ "embed"
	"fmt"
	"maps"
	"sync"

	"github.com/lixenwraith/terminal"
)

// KeyBehavior classifies how a key is processed.
type KeyBehavior uint8

const (
	BehaviorNone KeyBehavior = iota
	BehaviorMotion
	BehaviorCharWait
	BehaviorOperator
	BehaviorPrefix
	BehaviorPrefixMacro // @ prefix → StateMacroPlayAwait (decouples from key value)
	BehaviorModeSwitch
	BehaviorSpecial
	BehaviorSystem
	BehaviorAction
	BehaviorMarkerStart // g+direction triggers marker show, transitions to color await
)

// KeyEntry describes a key's behavior without function pointers.
type KeyEntry struct {
	Behavior   KeyBehavior
	Motion     MotionOp
	Special    SpecialOp
	ModeTarget ModeTarget
	IntentType IntentType
}

// KeyTable maps keys to behaviors for all modes.
type KeyTable struct {
	// Special keys (Ctrl+*, arrows, function keys)
	SpecialKeys map[terminal.Key]KeyEntry

	// Normal mode rune bindings
	NormalRunes map[rune]KeyEntry

	// Motions valid after operator (d)
	OperatorMotions map[rune]KeyEntry

	// Keys after g prefix
	PrefixG map[rune]KeyEntry

	// Overlay mode bindings
	OverlayRunes map[rune]KeyEntry
	OverlayKeys  map[terminal.Key]KeyEntry

	// Text mode navigation keys (Insert/Search/Command)
	TextNavKeys map[terminal.Key]KeyEntry
}

//go:embed default_keymap.toml
var defaultKeymapTOML []byte

// The embedded document is source-controlled and validated by package tests.
// Panicking on a broken build artifact matches the other embedded defaults.
var defaultKeyTable = sync.OnceValue(func() *KeyTable {
	kt, err := LoadKeyConfig(defaultKeymapTOML)
	if err != nil {
		panic(fmt.Sprintf("input: embedded default keymap: %v", err))
	}
	return kt
})

// DefaultKeyTable returns an independent copy of the embedded default bindings.
func DefaultKeyTable() *KeyTable { return defaultKeyTable().Clone() }

// Clone returns a deep copy of the KeyTable with independent maps.
func (kt *KeyTable) Clone() *KeyTable {
	return &KeyTable{
		SpecialKeys:     cloneKeyMap(kt.SpecialKeys),
		NormalRunes:     cloneRuneMap(kt.NormalRunes),
		OperatorMotions: cloneRuneMap(kt.OperatorMotions),
		PrefixG:         cloneRuneMap(kt.PrefixG),
		OverlayRunes:    cloneRuneMap(kt.OverlayRunes),
		OverlayKeys:     cloneKeyMap(kt.OverlayKeys),
		TextNavKeys:     cloneKeyMap(kt.TextNavKeys),
	}
}

func cloneRuneMap(m map[rune]KeyEntry) map[rune]KeyEntry {
	c := make(map[rune]KeyEntry, len(m))
	maps.Copy(c, m)
	return c
}

func cloneKeyMap(m map[terminal.Key]KeyEntry) map[terminal.Key]KeyEntry {
	c := make(map[terminal.Key]KeyEntry, len(m))
	maps.Copy(c, m)
	return c
}
