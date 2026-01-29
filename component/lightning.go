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
	LightningGold                             // positive energy vampire drain
	LightningGreen                            // Future: something
	LightningPurple                           // negative energy vampire drain
)

// LightningComponent represents a transient electrical effect between two points
type LightningComponent struct {
	// Endpoint positions - used as fallback when entity is 0
	OriginX, OriginY int
	TargetX, TargetY int

	// Entity tracking - resolved each frame by renderer (0 = use static position)
	OriginEntity core.Entity
	TargetEntity core.Entity

	// Visual color variant (indexes into renderer LUTs)
	ColorType LightningColorType

	// Path determinism
	PathSeed  uint64 // Seed for fractal path generation
	AnimFrame uint32 // Incremented each tick for tracked mode dancing

	// Animation state
	Remaining time.Duration
	Duration  time.Duration

	Owner core.Entity // Source entity for lifecycle management (despawn lookup)
}