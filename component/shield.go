package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// ShieldComponent represents a circular/elliptical energy shield or halo
// It serves as both a visual definition for the ShieldRenderer and a logic marker for physics systems
type ShieldComponent struct {
	Active bool

	// Visual Configuration
	Color      terminal.RGB
	Palette256 uint8 // 256-color palette index

	// Glow Effect (Optional)
	GlowColor     terminal.RGB
	GlowIntensity float64       // Peak glow alpha (0.0 to 1.0)
	GlowPeriod    time.Duration // Rotation duration (0 = disabled)

	// Physics/Geometry
	MaxOpacity    float64   // Maximum opacity at center (0.0 to 1.0)
	LastDrainTime time.Time // Last passive drain tick (for player shield)

	// Q32.32 fixed-point geometry
	RadiusX int64
	RadiusY int64
	InvRxSq int64 // Precomputed 1/RadiusX^2
	InvRySq int64 // Precomputed 1/RadiusY^2
}