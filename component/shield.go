package component

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

	// Q32.32 fixed-point values (set by ShieldSystem on creation/activation)
	RadiusX int64 // Q32.32
	RadiusY int64 // Q32.32

	// Precomputed Q32.32 inverse radii squared for ellipse checks
	// Set by ShieldSystem on activation/creation
	InvRxSq int64
	InvRySq int64
}