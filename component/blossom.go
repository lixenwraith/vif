package component

// BlossomComponent represents a blossom character entity
// Moves upward with acceleration, increases character levels on collision
type BlossomComponent struct {
	// Sub-pixel position (physics/render precision)
	// Grid position managed by PositionComponent (external)
	PreciseX float64
	PreciseY float64

	// Velocity
	// TODO: speed x, speed y
	Speed float64 // Current speed in rows per second
	// TODO: acceleration x, acceleration y
	Acceleration float64 // Speed increase per second

	// Visual
	Char          rune // The character being displayed
	LastChangeRow int  // Last row where character changed (Matrix effect)

	// Coordinate Latching (prevents re-processing same cell)
	LastIntX int // Last processed grid X coordinate
	LastIntY int // Last processed grid Y coordinate

	// Physics History (for swept collision detection)
	PrevPreciseX float64 // Previous frame X position
	PrevPreciseY float64 // Previous frame Y position
}