package component

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// CleanerColorType determines cleaner visual gradient
type CleanerColorType uint8

const (
	CleanerColorPositive CleanerColorType = iota // Yellow (energy >= 0)
	CleanerColorNegative                         // Violet (energy < 0)
	CleanerColorNugget                           // Green, targets green glyphs
)

type CleanerComponent struct {
	OwnerEntity core.Entity

	// Destruction target (tail must clear screen)
	TargetX float64
	TargetY float64

	// Ring buffer trail (zero-allocation updates)
	TrailRing [parameter.CleanerTrailLength]vmath.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	// Character used to render the cleaner block
	Rune rune

	// Color type for renderer gradient selection
	ColorType CleanerColorType

	// Blocking state: head stopped, trail draining to stop point
	Blocked        bool
	DrainSpeed     float64 // Absolute velocity for drain rate
	DrainRemaining float64 // Remaining drain distance; entity destroyed at 0
	DrainTotal     float64 // Initial drain distance (renderer ratio = Remaining/Total)
}
