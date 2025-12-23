package component

// BlossomComponent represents a blossom character entity
// Moves upward with acceleration, increases character levels on collision
type BlossomComponent struct {
	// Fixed-point positions and movement (Q16.16)
	// Sub-pixel position (physics/render precision), Grid position managed by PositionComponent
	PreciseX int32
	PreciseY int32

	// Velocity
	VelX int32
	VelY int32

	// Acceleration
	AccelX int32
	AccelY int32

	// Visual
	Char rune // The character being displayed

	// Coordinate Latching (prevents re-processing same cell)
	LastIntX int // Last processed grid X coordinate
	LastIntY int // Last processed grid Y coordinate

	// Physics History (for swept collision detection)
	PrevPreciseX float64 // Previous frame X position
	PrevPreciseY float64 // Previous frame Y position
}