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

// TracerCardinals is the spawn order of a volley's directions
var TracerCardinals = [...]TracerDirection{TracerRight, TracerLeft, TracerDown, TracerUp}

// tracerLanes are the row or column offsets a volley covers, by charge count
var tracerLanes = [...][]int{{0}, {-1, 1}, {-1, 0, 1}}

// TracerLanes are the offsets a volley of charges covers; charges past the table cover every lane
func TracerLanes(charges int) []int { return tracerLanes[max(min(charges, len(tracerLanes)), 1)-1] }

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
