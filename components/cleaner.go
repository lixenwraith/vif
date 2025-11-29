package components

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// CleanerTrailCapacity matches constants.CleanerTrailLength for fixed-size ring buffer
const CleanerTrailCapacity = 10

// CleanerComponent tracks cleaner entity movement and trail
type CleanerComponent struct {
	// Physics state (sub-pixel precision for smooth animation)
	PreciseX float64
	PreciseY float64

	// Movement vector (pixels per second)
	VelocityX float64
	VelocityY float64

	// Target coordinates where the entity should be destroyed
	// Used to ensure the tail clears the screen before removal
	TargetX float64
	TargetY float64

	// Trail history for rendering (integer grid coordinates)
	// Index 0 is the current head position
	Trail []core.Point

	// Current grid position helper (to detect cell changes)
	GridX int
	GridY int

	// Character used to render the cleaner block
	Char rune
}

// TODO: Update to ring buffer trail
// // CleanerComponent tracks cleaner entity movement and trail
// type CleanerComponent struct {
// 	// Physics state (sub-pixel precision for smooth animation)
// 	PreciseX float64
// 	PreciseY float64
//
// 	// Velocity (pixels per second)
// 	VelocityX float64
// 	VelocityY float64
//
// 	// Target position (for lifecycle management)
// 	TargetX float64
// 	TargetY float64
//
// 	// Last integer grid position (for trail updates)
// 	GridX int
// 	GridY int
//
// 	// Fixed-size ring buffer trail (zero allocation during updates)
// 	TrailRing [CleanerTrailCapacity]core.Point
// 	TrailHead int // Index of newest point
// 	TrailLen  int // Current number of points in trail (0 to CleanerTrailCapacity)
// }