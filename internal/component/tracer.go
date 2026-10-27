package component

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TracerDirection is one cardinal a gun volley covers; a kind's row holds a mask
type TracerDirection uint8

const (
	TracerRight TracerDirection = 1 << iota
	TracerLeft
	TracerDown
	TracerUp
	TracerAllDirections = TracerRight | TracerLeft | TracerDown | TracerUp
)

// TracerDirectionNames are what a document calls each direction, and all four
var TracerDirectionNames = map[string]TracerDirection{
	"right": TracerRight, "left": TracerLeft, "down": TracerDown, "up": TracerUp, "all": TracerAllDirections,
}

// TracerCardinals is the spawn order of a volley's directions
var TracerCardinals = [...]TracerDirection{TracerRight, TracerLeft, TracerDown, TracerUp}

// TracerComponent is one streak of the gun: a head sweeping a row or column at
// constant speed with a fading trail, draining where a wall or combatant stops it
type TracerComponent struct {
	OwnerEntity core.Entity

	// Destruction target (tail must clear screen)
	TargetX float64
	TargetY float64

	// Ring buffer trail (zero-allocation updates)
	TrailRing [parameter.TracerTrailLength]vmath.Point
	TrailHead int // Most recent point index
	TrailLen  int // Valid point count

	Palette WeaponPalette // the cursor's polarity when fired: selects the glyph effect and the gradient

	// Blocking state: head stopped, trail draining to stop point
	Blocked        bool
	DrainSpeed     float64 // Absolute velocity for drain rate
	DrainRemaining float64 // Remaining drain distance; entity destroyed at 0
	DrainTotal     float64 // Initial drain distance (renderer ratio = Remaining/Total)
}
