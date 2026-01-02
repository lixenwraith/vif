package component
// @lixen: #dev{feature[dust(render,system)]}

import (
	"time"
)

// ShieldComponent represents a circular/elliptical energy shield
// It is a geometric field effect that modifies visual rendering and physics interactions
// Shield is active when Sources != 0 AND Energy > 0
type ShieldComponent struct {
	Active bool

	MaxOpacity    float64   // Maximum opacity at center (0.0 to 1.0)
	LastDrainTime time.Time // Last passive drain tick (for 1/sec cost)

	// Q16.16 fixed-point values (set by ShieldSystem on creation/activation)
	RadiusX int32 // Q16.16
	RadiusY int32 // Q16.16

	// Precomputed Q16.16 inverse radii squared for ellipse checks
	// Set by ShieldSystem on activation/creation
	InvRxSq int32
	InvRySq int32
}