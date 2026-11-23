package visual

import "github.com/lixenwraith/color"

// A slide pulse draws as a swell SlideBand columns wide behind its edge, rising
// SlideRise columns off the surface: its slopes are lit from SlideLightX, Y, Z,
// what lies under it is bent by SlideLens columns a unit of slope as through a
// lens, and its body is a haze from RgbSlideFront at its edge to RgbSlideWake,
// both near the theme's background, so it reads as the scene itself bending
const (
	SlideBand  = 26.0
	SlideRise  = 7.0
	SlideSkew  = 0.35 // how far the crest leans from mid-band toward the edge
	SlideLens  = 6.0
	SlideGlow  = 0.5 // the body's share of the color at the crest
	SlideShade = 0.7 // the least light a slope turned from the lamp keeps
	SlideGloss = 0.2 // the highlight where a slope mirrors the lamp

	SlideLightX, SlideLightY, SlideLightZ = -0.48, -0.56, 0.675
)

var (
	RgbSlideFront = color.RGB{R: 62, G: 72, B: 104}
	RgbSlideWake  = color.RGB{R: 52, G: 46, B: 86}
)
