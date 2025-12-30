// @lixen: #dev{feature[lightning(render)]}
package component

import "time"

// LightningColorType defines the visual color variant of a lightning bolt
// Used to index into renderer color LUTs without cyclic dependency
type LightningColorType uint8

const (
	LightningCyan   LightningColorType = iota // Default: energy drain effect (both negative and positive)
	LightningRed                              // Future: damage
	LightningGold                             // Future: positive energy drain
	LightningPurple                           // Future: negative energy drain
)

// LightningComponent represents a transient electrical effect between two points
// Used for the fuse animation sequence
type LightningComponent struct {
	// Start position
	OriginX, OriginY int

	// End position
	TargetX, TargetY int

	// Visual color variant (indexes into renderer LUTs)
	ColorType LightningColorType

	// Animation state
	Remaining time.Duration
	Duration  time.Duration
}