package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

// MaterializeComponent represents a converging beam effect toward a spawn target;
// its deadline timer runs the beam and completes it
type MaterializeComponent struct {
	// Target area (beams converge to this rectangle)
	TargetX    int // Top-left X
	TargetY    int // Top-left Y
	AreaWidth  int // Target width (1 = single column)
	AreaHeight int // Target height (1 = single row)

	// Visual parameters
	BeamWidth int // Beam thickness perpendicular to direction (1 = thin)

	// Type of entity being spawned (for completion event)
	Type SpawnType
}

// SpawnType identifies what a materialization announces; its completion event
// carries it, and a consumer acts only on its own kind
type SpawnType int

const (
	SpawnTypeDrain SpawnType = iota
	SpawnTypeSwarm
	SpawnTypeNugget // a beacon: nothing spawns when it completes
	SpawnTypeCount
)

// MaterializeStyle is one spawn type's beam timing; a zero field takes the default
type MaterializeStyle struct {
	Duration time.Duration
	FillEnd  float64 // fraction of Duration at which the fill ends and the hold begins
	HoldEnd  float64 // fraction at which the hold ends and the recede begins
}

// MaterializeStyles is indexed by SpawnType
var MaterializeStyles = [SpawnTypeCount]MaterializeStyle{
	SpawnTypeNugget: {Duration: parameter.MaterializeNuggetDuration,
		FillEnd: parameter.MaterializeNuggetFillEnd, HoldEnd: parameter.MaterializeNuggetHoldEnd},
}

// Style is the spawn type's beam timing with the defaults filled in
func (t SpawnType) Style() MaterializeStyle {
	var s MaterializeStyle
	if t >= 0 && t < SpawnTypeCount {
		s = MaterializeStyles[t]
	}
	if s.Duration <= 0 {
		s.Duration = parameter.MaterializeAnimationDuration
	}
	if s.FillEnd <= 0 {
		s.FillEnd = parameter.MaterializeFillEnd
	}
	if s.HoldEnd <= 0 {
		s.HoldEnd = parameter.MaterializeHoldEnd
	}
	return s
}
