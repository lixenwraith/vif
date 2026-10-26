package component

import (
	"time"
)

// ShieldType distinguishes shield configurations for visual lookup
type ShieldType uint8

const (
	ShieldTypePlayer ShieldType = iota
	ShieldTypeQuasar
	ShieldTypeLoot
	ShieldTypeCount
)

// ShieldComponent holds runtime state for game mechanics
// Visual parameters come from visual.ShieldConfigFor(Type, aspect)
type ShieldComponent struct {
	// DrainElapsed is how long a player's shield has been up since it rose or last drained
	DrainElapsed time.Duration

	RadiusX float64
	RadiusY float64
	InvRxSq float64
	InvRySq float64

	Type   ShieldType
	Active bool
}
