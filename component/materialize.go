package component

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// MaterializeDirection indicates which screen edge the spawner originates from
type MaterializeDirection int

const (
	MaterializeFromTop MaterializeDirection = iota
	MaterializeFromBottom
	MaterializeFromLeft
	MaterializeFromRight
)

// SpawnType identifies what entity will be spawned upon materialization completion
type SpawnType int

const (
	SpawnTypeDrain SpawnType = iota
	// Future: SpawnTypeNugget, SpawnTypeBot, etc.
)

// MaterializeComponent represents a spawner entity that converges toward a target position
type MaterializeComponent struct {
	// Sub-pixel position (physics/render precision)
	PreciseX float64
	PreciseY float64

	// Movement vector (pixels per second)
	VelocityX float64
	VelocityY float64

	// Target position (where spawners converge)
	TargetX int
	TargetY int

	// Ring buffer trail (zero-allocation updates)
	TrailRing [constant.MaterializeTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Direction this spawner came from
	Direction MaterializeDirection

	// Character used to render the spawner block
	Char rune

	// Arrived flag - set when spawner reaches target
	Arrived bool

	// Type of entity being spawned
	Type SpawnType
}