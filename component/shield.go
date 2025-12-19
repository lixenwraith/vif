package component

import (
	"time"
)

// ShieldComponent represents a circular/elliptical energy shield
// It is a geometric field effect that modifies visual rendering and physics interactions
// Shield is active when Sources != 0 AND Energy > 0
type ShieldComponent struct {
	Active        bool
	RadiusX       float64    // Horizontal radius in grid cells
	RadiusY       float64    // Vertical radius in grid cells
	OverrideColor ColorClass // ColorNone = derive from GameState, else use this color
	MaxOpacity    float64    // Maximum opacity at center (0.0 to 1.0)
	LastDrainTime time.Time  // Last passive drain tick (for 1/sec cost)
}