package components

import "github.com/gdamore/tcell/v2"

// ShieldComponent represents a circular/elliptical energy shield.
// It is a geometric field effect that modifies visual rendering and physics interactions.
type ShieldComponent struct {
	Active     bool
	RadiusX    float64     // Horizontal radius in grid cells
	RadiusY    float64     // Vertical radius in grid cells
	Color      tcell.Color // Base color of the shield
	MaxOpacity float64     // Maximum opacity at center (0.0 to 1.0)
}