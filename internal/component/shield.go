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
// the field, zero taking the kind's colour.
type ShieldComponent struct {
	// DrainElapsed is how long a player's shield has been up since it rose or last drained
	DrainElapsed time.Duration

	Radius float64
	Color  color.RGB

	Type   ShieldType
	Active bool
}
