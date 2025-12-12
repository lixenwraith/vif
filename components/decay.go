// @lixen: #focus{gameplay[obstacle,decay]}
// @lixen: #interact{state[decay]}
package components

// DecayComponent represents a decay character entity
type DecayComponent struct {
	// Sub-pixel position (physics/render precision)
	// Grid position managed by PositionComponent (external)
	PreciseX float64
	PreciseY float64

	// Velocity
	Speed float64 // Falling speed in rows per second

	// Visual
	Char          rune // The character being displayed
	LastChangeRow int  // Last row where character changed (Matrix effect)

	// Coordinate Latching (prevents re-processing same cell)
	LastIntX int // Last processed grid X coordinate
	LastIntY int // Last processed grid Y coordinate

	// Physics History (for swept collision detection)
	PrevPreciseX float64 // Previous frame X position (future: magnetic)
	PrevPreciseY float64 // Previous frame Y position
}