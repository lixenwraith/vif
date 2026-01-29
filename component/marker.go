package component

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// MarkerShape defines the visual representation
type MarkerShape uint8

const (
	MarkerShapeNone      MarkerShape = iota // Invisible area (logical only)
	MarkerShapeRectangle                    // Filled rectangle
	MarkerShapeInvert                       // Character fg/bg inversion (stub)
	// Future: MarkerShapeCircle, MarkerShapeCross, MarkerShapeDiamond, MarkerShapeRing
)

// MarkerComponent represents a visual area indicator
type MarkerComponent struct {
	X, Y      int // Top-left position
	Width     int // Area width (minimum 1)
	Height    int // Area height (minimum 1)
	Shape     MarkerShape
	Color     terminal.RGB
	Intensity int64 // Q32.32, 0-Scale for alpha/fade control
	PulseRate int64 // Q32.32, 0 = no pulse, >0 = Hz
	FadeMode  uint8 // 0=none, 1=fade out, 2=fade in
}