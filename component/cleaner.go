package component

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// CleanerColorType determines cleaner visual gradient
type CleanerColorType uint8

const (
	CleanerColorPositive CleanerColorType = iota // Yellow (energy >= 0)
	CleanerColorNegative                         // Violet (energy < 0)
	CleanerColorNugget                           // Green, targets green glyphs
)

// CleanerComponent tracks cleaner entity movement and trail
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

	// Color type for renderer gradient selection
	ColorType CleanerColorType
}