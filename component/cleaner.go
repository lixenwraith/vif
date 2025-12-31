package component
// @lixen: #dev{feature[drain(render,system)]}

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// CleanerComponent tracks cleaner entity movement and trail
// Grid position managed by PositionComponent
type CleanerComponent struct {
	KineticState // Embeds PreciseX, PreciseY, VelX, VelY, AccelX, AccelY

	// Destruction target (tail must clear screen) - Q16.16
	TargetX int32
	TargetY int32

	// Ring buffer trail (zero-allocation updates)
	TrailRing [constant.CleanerTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Character used to render the cleaner block
	Char rune

	// Energy polarity indicator for rendering
	NegativeEnergy bool // Set at spawn, determines gradient color
}