package components

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// ShieldComponent represents a circular/elliptical energy shield
// It is a geometric field effect that modifies visual rendering and physics interactions
// Shield is active when Sources != 0 AND Energy > 0
type ShieldComponent struct {
	Sources       uint8     // Bitmask of active sources (ShieldSourceBoost, etc)
	RadiusX       float64   // Horizontal radius in grid cells
	RadiusY       float64   // Vertical radius in grid cells
	Color         core.RGB  // Base color of the shield
	MaxOpacity    float64   // Maximum opacity at center (0.0 to 1.0)
	LastDrainTime time.Time // Last passive drain tick (for 1/sec cost)
}