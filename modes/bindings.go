package modes

import "github.com/lixenwraith/vi-fighter/engine"

// ActionType classifies key behaviors for state machine logic
type ActionType uint8

const (
	ActionNone       ActionType = iota
	ActionMotion                // h,j,k,l,w,b,0,$,etc - immediate execution
	ActionCharWait              // f,F,t,T - wait for target char
	ActionOperator              // d - wait for motion
	ActionPrefix                // g - wait for second key
	ActionModeSwitch            // i,/,: - change mode
	ActionSpecial               // x,D,n,N,;,, - immediate with special handling
)

// Binding maps a key to its behavior
type Binding struct {
	Action       ActionType
	Target       rune // Canonical command (for remapping)
	AcceptsCount bool
	Executor     func(*engine.GameContext, int)
}

// BindingTable holds all key bindings
type BindingTable struct {
	normal          map[rune]*Binding
	operatorMotions map[rune]*Binding
	prefixG         map[rune]*Binding
}

// DefaultBindings returns the default binding table
func DefaultBindings() *BindingTable {
	return &BindingTable{
		normal: map[rune]*Binding{
			// Basic motions
			'h': {ActionMotion, 'h', true, wrapMotion('h')},
			'j': {ActionMotion, 'j', true, wrapMotion('j')},
			'k': {ActionMotion, 'k', true, wrapMotion('k')},
			'l': {ActionMotion, 'l', true, wrapMotion('l')},
			' ': {ActionMotion, ' ', true, wrapMotion(' ')},

			// Word motions
			'w': {ActionMotion, 'w', true, wrapMotion('w')},
			'W': {ActionMotion, 'W', true, wrapMotion('W')},
			'b': {ActionMotion, 'b', true, wrapMotion('b')},
			'B': {ActionMotion, 'B', true, wrapMotion('B')},
			'e': {ActionMotion, 'e', true, wrapMotion('e')},
			'E': {ActionMotion, 'E', true, wrapMotion('E')},

			// Line motions
			'0': {ActionMotion, '0', false, wrapMotion('0')},
			'^': {ActionMotion, '^', false, wrapMotion('^')},
			'$': {ActionMotion, '$', true, wrapMotion('$')},

			// Screen motions
			'H': {ActionMotion, 'H', true, wrapMotion('H')},
			'M': {ActionMotion, 'M', false, wrapMotion('M')},
			'L': {ActionMotion, 'L', true, wrapMotion('L')},
			'G': {ActionMotion, 'G', true, wrapMotion('G')},

			// Paragraph motions
			'{': {ActionMotion, '{', true, wrapMotion('{')},
			'}': {ActionMotion, '}', true, wrapMotion('}')},

			// Bracket matching
			'%': {ActionMotion, '%', false, wrapMotion('%')},

			// Char-wait commands
			'f': {ActionCharWait, 'f', true, nil},
			'F': {ActionCharWait, 'F', true, nil},
			't': {ActionCharWait, 't', true, nil},
			'T': {ActionCharWait, 'T', true, nil},

			// Operator
			'd': {ActionOperator, 'd', true, nil},

			// Prefix
			'g': {ActionPrefix, 'g', true, nil},

			// Mode switches
			'i': {ActionModeSwitch, 'i', false, nil},
			'/': {ActionModeSwitch, '/', false, nil},
			':': {ActionModeSwitch, ':', false, nil},

			// Special commands
			'x': {ActionSpecial, 'x', true, execDeleteChar},
			'D': {ActionSpecial, 'D', true, execDeleteToEOL},
			'n': {ActionSpecial, 'n', false, execSearchNext},
			'N': {ActionSpecial, 'N', false, execSearchPrev},
			';': {ActionSpecial, ';', true, execRepeatFind},
			',': {ActionSpecial, ',', true, execRepeatFindReverse},
		},
		operatorMotions: map[rune]*Binding{
			// Motions valid after 'd'
			'w': {ActionMotion, 'w', true, nil},
			'W': {ActionMotion, 'W', true, nil},
			'b': {ActionMotion, 'b', true, nil},
			'B': {ActionMotion, 'B', true, nil},
			'e': {ActionMotion, 'e', true, nil},
			'E': {ActionMotion, 'E', true, nil},
			'0': {ActionMotion, '0', false, nil},
			'^': {ActionMotion, '^', false, nil},
			'$': {ActionMotion, '$', true, nil},
			'd': {ActionMotion, 'd', true, nil}, // dd
			'G': {ActionMotion, 'G', true, nil},
			'{': {ActionMotion, '{', true, nil},
			'}': {ActionMotion, '}', true, nil},
			'%': {ActionMotion, '%', false, nil},
			// Char motions after operator
			'f': {ActionCharWait, 'f', true, nil},
			'F': {ActionCharWait, 'F', true, nil},
			't': {ActionCharWait, 't', true, nil},
			'T': {ActionCharWait, 'T', true, nil},
		},
		prefixG: map[rune]*Binding{
			'g': {ActionMotion, 'g', true, execGotoTop},
			'o': {ActionMotion, 'o', true, execGotoOrigin},
		},
	}
}

// LoadBindings loads bindings from config file (placeholder)
// Format: JSON/TOML key-value where key is source rune, value is target behavior
func LoadBindings(path string) (*BindingTable, error) {
	// TODO: implement config loading
	return DefaultBindings(), nil
}

// wrapMotion creates an executor that calls ExecuteMotion
func wrapMotion(cmd rune) func(*engine.GameContext, int) {
	return func(ctx *engine.GameContext, count int) {
		ExecuteMotion(ctx, cmd, count)
	}
}
