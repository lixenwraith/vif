package visual

import "github.com/lixenwraith/color"

// A slide pulse's ripple: the SlideBand columns behind its edge are tiles in turn,
// lit toward RgbSlideLight by up to SlideShine as they tilt and shaded by up to
// SlideShade once past edge-on, with RgbSlideGap between them
const (
	SlideBand  = 14.0
	SlideShine = 0.85
	SlideShade = 0.6
)

var (
	RgbSlideLight = color.RGB{R: 225, G: 235, B: 255}
	RgbSlideGap   = color.RGB{R: 6, G: 7, B: 10}
)
