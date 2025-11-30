package modes

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
	Action     ActionType
	Target     rune           // Canonical command identifier
	Motion     MotionFunc     // For standard motions
	CharMotion CharMotionFunc // For f/F/t/T
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
			'h': {ActionMotion, 'h', MotionLeft, nil},
			'j': {ActionMotion, 'j', MotionDown, nil},
			'k': {ActionMotion, 'k', MotionUp, nil},
			'l': {ActionMotion, 'l', MotionRight, nil},
			' ': {ActionMotion, ' ', MotionRight, nil},

			// Word motions
			'w': {ActionMotion, 'w', MotionWordForward, nil},
			'W': {ActionMotion, 'W', MotionWORDForward, nil},
			'b': {ActionMotion, 'b', MotionWordBack, nil},
			'B': {ActionMotion, 'B', MotionWORDBack, nil},
			'e': {ActionMotion, 'e', MotionWordEnd, nil},
			'E': {ActionMotion, 'E', MotionWORDEnd, nil},

			// Line motions
			'0': {ActionMotion, '0', MotionLineStart, nil},
			'^': {ActionMotion, '^', MotionFirstNonWS, nil},
			'$': {ActionMotion, '$', MotionLineEnd, nil},

			// Screen motions
			'H': {ActionMotion, 'H', MotionScreenTop, nil},
			'M': {ActionMotion, 'M', MotionScreenMid, nil},
			'L': {ActionMotion, 'L', MotionScreenBot, nil},
			'G': {ActionMotion, 'G', MotionFileEnd, nil},

			// Paragraph motions
			'{': {ActionMotion, '{', MotionParaBack, nil},
			'}': {ActionMotion, '}', MotionParaForward, nil},

			// Bracket matching
			'%': {ActionMotion, '%', MotionMatchBracket, nil},

			// Char-wait commands
			'f': {ActionCharWait, 'f', nil, MotionFindForward},
			'F': {ActionCharWait, 'F', nil, MotionFindBack},
			't': {ActionCharWait, 't', nil, MotionTillForward},
			'T': {ActionCharWait, 'T', nil, MotionTillBack},

			// Operator
			'd': {ActionOperator, 'd', nil, nil},

			// Prefix
			'g': {ActionPrefix, 'g', nil, nil},

			// Mode switches
			'i': {ActionModeSwitch, 'i', nil, nil},
			'/': {ActionModeSwitch, '/', nil, nil},
			':': {ActionModeSwitch, ':', nil, nil},

			// Special commands
			'x': {ActionSpecial, 'x', nil, nil},
			'D': {ActionSpecial, 'D', nil, nil},
			'n': {ActionSpecial, 'n', nil, nil},
			'N': {ActionSpecial, 'N', nil, nil},
			';': {ActionSpecial, ';', nil, nil},
			',': {ActionSpecial, ',', nil, nil},
		},
		operatorMotions: map[rune]*Binding{
			// Motions valid after 'd'
			'w': {ActionMotion, 'w', MotionWordForward, nil},
			'W': {ActionMotion, 'W', MotionWORDForward, nil},
			'b': {ActionMotion, 'b', MotionWordBack, nil},
			'B': {ActionMotion, 'B', MotionWORDBack, nil},
			'e': {ActionMotion, 'e', MotionWordEnd, nil},
			'E': {ActionMotion, 'E', MotionWORDEnd, nil},
			'0': {ActionMotion, '0', MotionLineStart, nil},
			'^': {ActionMotion, '^', MotionFirstNonWS, nil},
			'$': {ActionMotion, '$', MotionLineEnd, nil},
			'G': {ActionMotion, 'G', MotionFileEnd, nil},
			'{': {ActionMotion, '{', MotionParaBack, nil},
			'}': {ActionMotion, '}', MotionParaForward, nil},
			'%': {ActionMotion, '%', MotionMatchBracket, nil},
			'h': {ActionMotion, 'h', MotionLeft, nil},
			'j': {ActionMotion, 'j', MotionDown, nil},
			'k': {ActionMotion, 'k', MotionUp, nil},
			'l': {ActionMotion, 'l', MotionRight, nil},
			' ': {ActionMotion, ' ', MotionRight, nil},
			// Char motions after operator
			'f': {ActionCharWait, 'f', nil, MotionFindForward},
			'F': {ActionCharWait, 'F', nil, MotionFindBack},
			't': {ActionCharWait, 't', nil, MotionTillForward},
			'T': {ActionCharWait, 'T', nil, MotionTillBack},
		},
		prefixG: map[rune]*Binding{
			'g': {ActionMotion, 'g', MotionFileStart, nil},
			'o': {ActionMotion, 'o', MotionOrigin, nil},
		},
	}
}