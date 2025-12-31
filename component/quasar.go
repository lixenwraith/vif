package component

// QuasarChars defines the 3×5 visual representation
var QuasarChars = [3][5]rune{
	{'╔', '═', '╦', '═', '╗'},
	{'╠', '═', '╬', '═', '╣'},
	{'╚', '═', '╩', '═', '╝'},
}

// QuasarComponent holds quasar-specific runtime state
// Composite structure managed via CompositeHeaderComponent
type QuasarComponent struct {
	TicksSinceLastMove  int
	TicksSinceLastSpeed int
	IsOnCursor          bool // True if any member overlaps cursor position

	// Speed scaling (Q16.16, starts at vmath.Scale = 1.0)
	SpeedMultiplier int32

	// Zap state
	IsZapping bool
}