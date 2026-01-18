package component

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
)

// CleanerComponent tracks cleaner entity movement and trail
// Grid position managed by PositionComponent
type CleanerComponent struct {
	Kinetic // Embeds PreciseX, PreciseY, VelX, VelY, AccelX, AccelY

	// Destruction target (tail must clear screen) - Q32.32
	TargetX int64
	TargetY int64

	// Ring buffer trail (zero-allocation updates)
	TrailRing [constant.CleanerTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Character used to render the cleaner block
	Char rune

	// Energy polarity indicator for rendering
	NegativeEnergy bool // SetPosition at spawn, determines gradient color
}