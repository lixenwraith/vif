// @lixen: #dev{feature[spirit(render,system)]}
package component

import "time"

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// QuasarComponent holds quasar-specific runtime state
// Composite structure managed via CompositeHeaderComponent
type QuasarComponent struct {
	LastMoveTime time.Time
	IsOnCursor   bool // True if any member overlaps cursor position
}