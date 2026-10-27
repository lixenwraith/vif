package component

import "github.com/lixenwraith/vif/internal/core"

// LightningColorType defines the visual color variant of a lightning bolt, indexed to renderer color LUT
type LightningColorType uint8

const (
	LightningCyan   LightningColorType = iota // Default: convergent energy drain effect
	LightningRed                              // Future: damage
	LightningGold                             // Positive energy drain
	LightningGreen                            // Future: something
	LightningPurple                           // Negative energy drain
)

// LightningColorNames are what a document calls each bolt colour
var LightningColorNames = [...]string{
	LightningCyan:   "cyan",
	LightningRed:    "red",
	LightningGold:   "gold",
	LightningGreen:  "green",
	LightningPurple: "purple",
}

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

	// Tracked bolts live on a lifetime lease their owner renews; others on a fixed lifetime
	Tracked bool

	Owner core.Entity // Source entity for lifecycle management (despawn lookup)
}
