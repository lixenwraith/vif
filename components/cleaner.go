// @focus: #core { ecs, types } #game { cleaner } #render { effects }
package components

import (
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
)

// CleanerComponent tracks cleaner entity movement and trail
// Grid position managed by PositionComponent (external)
type CleanerComponent struct {
	// Sub-pixel position for smooth animation
	PreciseX float64
	PreciseY float64

	// Movement vector (pixels/second)
	VelocityX float64
	VelocityY float64

	// Destruction target (tail must clear screen)
	TargetX float64
	TargetY float64

	// Ring buffer trail (zero-allocation updates)
	TrailRing [constants.CleanerTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Character used to render the cleaner block
	Char rune
}