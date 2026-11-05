package component

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// MaterializeComponent is a materialize over a body's footprint with its header on
// X, Y, which its deadline timer runs and completes
type MaterializeComponent struct {
	X, Y      int
	Footprint vmath.Footprint
	Type      SpawnType // the style it plays, which its completion carries
	// Species is the species its end lands, so the materialize itself holds the
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

// SpawnTypeDataFirst is the first program's style: a scenario's programs take
// SpawnTypeDataFirst onward in sorted-name order, as their species do
const SpawnTypeDataFirst = SpawnTypeCount

// MaterializeStyle is one spawn type: the name a document calls it by, its beams'
// colour, a program's alone, as the renderer holds a code style's, and its timing;
// the fill runs each beam's head from the edge to the target, the hold keeps the
// beam, the recede runs the tail after it
type MaterializeStyle struct {
	Name               string
	Color              color.RGB
	Fill, Hold, Recede time.Duration
}

// Duration is the whole materialize, which its deadline timer runs
func (s MaterializeStyle) Duration() time.Duration { return s.Fill + s.Hold + s.Recede }

// MaterializeStyles is indexed by SpawnType
var MaterializeStyles = [SpawnTypeCount]MaterializeStyle{
	SpawnTypeDrain:  {Name: "drain", Fill: parameter.MaterializeFill, Hold: parameter.MaterializeHold, Recede: parameter.MaterializeRecede},
	SpawnTypeSwarm:  {Name: "swarm", Fill: parameter.MaterializeFill, Hold: parameter.MaterializeHold, Recede: parameter.MaterializeRecede},
	SpawnTypeNugget: {Name: "nugget", Fill: parameter.MaterializeNuggetFill, Hold: parameter.MaterializeNuggetHold, Recede: parameter.MaterializeNuggetRecede},
}
