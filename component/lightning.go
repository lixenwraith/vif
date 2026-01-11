package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// LightningColorType defines the visual color variant of a lightning bolt, indexed to renderer color LUT
type LightningColorType uint8

const (
	LightningCyan   LightningColorType = iota // Default: convergent energy drain effect
	LightningRed                              // Future: damage
	LightningGold                             // Future: positive energy drain
	LightningPurple                           // Future: negative energy drain
)

// LightningComponent represents a transient electrical effect between two points
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

	Owner core.Entity // Source entity for lifecycle management (0 = unowned)
}