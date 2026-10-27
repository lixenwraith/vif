package component

import (
	"github.com/lixenwraith/color"
)

// MarkerShape defines the visual representation
type MarkerShape uint8

const (
	MarkerShapeNone      MarkerShape = iota // Invisible area (logical only)
	MarkerShapeRectangle                    // Filled rectangle
	MarkerShapeInvert                       // Character fg/bg inversion (stub)
	// Future: MarkerShapeCircle, MarkerShapeCross, MarkerShapeDiamond, MarkerShapeRing
)

// MarkerShapeNames are what a document calls each MarkerShape
var MarkerShapeNames = [...]string{MarkerShapeNone: "none", MarkerShapeRectangle: "rectangle", MarkerShapeInvert: "invert"}

// MarkerFade is how a marker's intensity changes over its lifetime
type MarkerFade uint8

const (
	MarkerFadeNone MarkerFade = iota
	MarkerFadeOut             // over the marker's remaining lifetime
	MarkerFadeIn              // unimplemented: reads as none
)

// MarkerFadeNames are what a document calls each MarkerFade
var MarkerFadeNames = [...]string{MarkerFadeNone: "none", MarkerFadeOut: "out", MarkerFadeIn: "in"}

// MarkerComponent represents a visual area indicator
type MarkerComponent struct {
	X, Y      int // Top-left position
	Width     int // Area width (minimum 1)
	Height    int // Area height (minimum 1)
	Shape     MarkerShape
	Color     color.RGB
	Intensity float64 // 0.0-1.0 for alpha/fade control
	PulseRate float64 // 0 = no pulse, >0 = Hz
	FadeMode  MarkerFade
}
