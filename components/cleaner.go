package components

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// CleanerComponent represents a horizontal line-clearing animation entity.
// Cleaners sweep across rows containing Red characters, removing them on contact.
type CleanerComponent struct {
	// Physics state (sub-pixel precision)
	PreciseX float64
	PreciseY float64

	// Movement vector (units per second)
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
