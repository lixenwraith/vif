package visual

import "github.com/lixenwraith/color"

// The lixen mark (lixen-logo.svg) across its viewBox, a square side 1: strokes LogoStroke
// wide ring the square and cross it straight and diagonally, cutting eight wedges.
// LogoSamples is the samples a side of a quadrant of a cell its drawing takes.
const (
	LogoStroke  = 1.0 / 11
	LogoSamples = 6
)

// The wedges as the SVG colors them, clockwise from the bottom edge's left half, then the
// strokes; in 256 colors the console entries that keep the order apart
var (
	RgbLogoWedges = [8]color.RGB{
		color.Red, color.Orange, color.Yellow, {G: 128},
		color.Blue, {R: 75, B: 130}, {R: 238, G: 130, B: 238}, color.MidGray,
	}
	RgbLogoStroke = color.Black

	LogoWedges256 = [8]uint8{
		ConBrightRed, ConYellow, ConBrightYellow, ConGreen,
		ConBlue, ConMagenta, ConBrightMagenta, ConWhite,
	}
	LogoStroke256 = ConGray
)
