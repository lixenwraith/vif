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
	Active bool
	Type   ShieldType

	// Q32.32 fixed-point geometry (game mechanics)
	RadiusX int64
	RadiusY int64
	InvRxSq int64
	InvRySq int64

	// Player-specific runtime state
	LastDrainTime time.Time
}