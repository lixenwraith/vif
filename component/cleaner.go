package component

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// CleanerComponent tracks cleaner entity movement and trail
// Grid position managed by PositionComponent
type CleanerComponent struct {
	// Destruction target (tail must clear screen) - Q32.32
	TargetX int64
	TargetY int64

	// Ring buffer trail (zero-allocation updates)
	TrailRing [parameter.CleanerTrailLength]core.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Character used to render the cleaner block
	Rune rune

	// Energy polarity indicator for rendering
	NegativeEnergy bool // Set at spawn, determines gradient color
}