package component

import (
	"time"

	"github.com/lixenwraith/color"
)

// ShieldType names a shield's kind, whose look visual.ShieldLooks holds
type ShieldType uint8

const (
	ShieldTypePlayer ShieldType = iota
	ShieldTypeQuasar
	ShieldTypeLoot
	ShieldTypeCount
)

// ShieldComponent is a shield's state. Radius is its reach in columns, its rows the
// cell aspect's (vmath.Aspect.FromCircular), for contact and drawing alike, so
// whatever sizes a shield writes Radius alone; zero reaches no cell. Color tints
// the field, zero taking the kind's color.
type ShieldComponent struct {
	// DrainElapsed is how long a player's shield has been up since it rose or last drained
	DrainElapsed time.Duration

	// BoostCap is the most boost the shield's cursor banks, zero for no cap
	BoostCap time.Duration

	// Step is the energy a step of visual.ShieldRamp spans: a shield with one is
	// colored by what it holds, keeps it with no passive drain, and holds no more
	// than parameter.ShieldSteps of them; zero is main's
	Step int64

	Radius float64
	Color  color.RGB

	Type   ShieldType
	Active bool

	// Stemless draws its cursor's rays from their orbs out, clear of what it covers
	Stemless bool
}
