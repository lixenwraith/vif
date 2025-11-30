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

// MaterializeComponent represents a spawner entity that converges toward a target position
// Used for drain materialization animation (4 spawners converge from screen edges)
// Multiple sets of materializers can be active simultaneously (grouped by DrainSlot)
type MaterializeComponent struct {
	// Physics state (sub-pixel precision)
	PreciseX float64
	PreciseY float64

	// Movement vector (pixels per second)
	VelocityX float64
	VelocityY float64

	// Target position (where spawners converge)
	TargetX int
	TargetY int

	// Ring buffer trail (zero-allocation updates)
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

	// Drain slot association (-1 for non-drain materializers, 0-9 for drain slots)
	DrainSlot int
}