package component

import "time"

// LightningComponent represents a transient electrical effect between two points
// Used for the fuse animation sequence
type LightningComponent struct {
	// Start position
	OriginX, OriginY int

	// End position
	TargetX, TargetY int

	// Animation state
	Remaining time.Duration
	Duration  time.Duration
}