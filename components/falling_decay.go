package components

// FallingDecayComponent represents a falling decay character entity
type FallingDecayComponent struct {
	Column        int     // X position (fixed column)
	YPosition     float64 // Current Y position (float for smooth speeds)
	Speed         float64 // Falling speed in rows per second
	Char          rune    // The character being displayed
	LastChangeRow int     // Last row where character changed (for Matrix-style animation)
}
