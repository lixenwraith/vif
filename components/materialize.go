package components

import (
	"github.com/lixenwraith/vi-fighter/constants"
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

// MaterializeComponent represents a converging spawn animation entity
// Four spawners (one per edge) converge to a target position before
// the actual drain entity materializes
type MaterializeComponent struct {
	// Physics state (sub-pixel precision)
	PreciseX float64
	PreciseY float64

	// Movement vector (units per second)
	VelocityX float64
	VelocityY float64

	// Target coordinates where spawner converges
	TargetX int
	TargetY int

	// Ring buffer trail (zero-allocation updates)
	// Index 0 is the current head position
	TrailRing [constants.MaterializeTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Current grid position (for detecting cell changes)
	GridX int
	GridY int

	// Direction this spawner came from
	Direction MaterializeDirection

	// Character used to render the spawner block
	Char rune

	// Arrived flag - set when spawner reaches target
	Arrived bool
}