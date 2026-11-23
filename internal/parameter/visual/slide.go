package visual

import "github.com/lixenwraith/color"

// A slide pulse draws as a swell SlideBand columns wide behind its edge, rising
// SlideRise columns off the surface: its slopes are lit from SlideLightX, Y, Z,
// what lies under it is bent by SlideLens columns a unit of slope as through a
// lens, and its body glows from RgbSlideFront at its edge to RgbSlideWake
const (
	SlideBand  = 26.0
	SlideRise  = 7.0
	SlideSkew  = 0.35 // how far the crest leans from mid-band toward the edge
	SlideLens  = 3.5
	SlideGlow  = 0.85 // the body's share of the color at the crest
	SlideShade = 0.45 // the least light a slope turned from the lamp keeps
	SlideGloss = 0.9  // the highlight where a slope mirrors the lamp

	SlideLightX, SlideLightY, SlideLightZ = -0.48, -0.56, 0.675
)

var (
	RgbSlideFront = color.RGB{R: 70, G: 190, B: 255}
	RgbSlideWake  = color.RGB{R: 150, G: 60, B: 255}
)
