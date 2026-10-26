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
	// Species is the species the completion lands, so the beam itself holds the
	// spawn; SpeciesNone lands nothing
	Species SpeciesType
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

// MaterializeStyle is one spawn type's beam timing: the fill runs the head from
// the edge to the target, the hold keeps the line, the recede runs the tail after it
type MaterializeStyle struct {
	Fill, Hold, Recede time.Duration
}

// Duration is the whole beam, which its deadline timer runs
func (s MaterializeStyle) Duration() time.Duration { return s.Fill + s.Hold + s.Recede }

// MaterializeStyles is indexed by SpawnType
var MaterializeStyles = [SpawnTypeCount]MaterializeStyle{
	SpawnTypeDrain:  {Fill: parameter.MaterializeFill, Hold: parameter.MaterializeHold, Recede: parameter.MaterializeRecede},
	SpawnTypeSwarm:  {Fill: parameter.MaterializeFill, Hold: parameter.MaterializeHold, Recede: parameter.MaterializeRecede},
	SpawnTypeNugget: {Fill: parameter.MaterializeNuggetFill, Hold: parameter.MaterializeNuggetHold, Recede: parameter.MaterializeNuggetRecede},
}
