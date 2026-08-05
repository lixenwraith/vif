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
)

// ShieldComponent holds runtime state for game mechanics
// Visual parameters looked up via Type in visual.ShieldConfigs
type ShieldComponent struct {
	// Player-specific runtime state
	LastDrainTime time.Time

	RadiusX float64
	RadiusY float64
	InvRxSq float64
	InvRySq float64

	Type   ShieldType
	Active bool
}
