package components

// FallingDecayComponent represents a falling decay character entity
type FallingDecayComponent struct {
	// Position (float for smooth movement, future magnetic support)
	Column        int     // Current column (legacy, keep for compatibility)
	YPosition     float64 // Current Y position

	// Velocity
	Speed         float64 // Falling speed in rows per second

	// Visual
	Char          rune    // The character being displayed
	LastChangeRow int     // Last row where character changed (Matrix effect)

	// Coordinate Latching (prevents re-processing same cell)
	LastIntX      int     // Last processed grid X coordinate
	LastIntY      int     // Last processed grid Y coordinate

	// Physics History (for swept collision detection)
	PrevPreciseX  float64 // Previous frame X position (future: magnetic)
	PrevPreciseY  float64 // Previous frame Y position
}
