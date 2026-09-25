package visual

import "github.com/lixenwraith/color"

// The sixteen colors of a text console, which 256-color mode draws for; a 256-color table names
// these to get exactly the console color it means. On a console with eight backgrounds, a bright
// entry drawn as a background shows as its normal counterpart.
const (
	ConBlack uint8 = iota
	ConRed
	ConGreen
	ConYellow
	ConBlue
	ConMagenta
	ConCyan
	ConWhite
	ConGray
	ConBrightRed
	ConBrightGreen
	ConBrightYellow
	ConBrightBlue
	ConBrightMagenta
	ConBrightCyan
	ConBrightWhite
)

// Heat256LUT holds the heat bar's ten segments, red through purple; eight backgrounds leave
// six hues in the same order
var Heat256LUT = [10]uint8{
	ConRed, ConBrightRed, ConYellow, ConBrightYellow, ConGreen,
	ConBrightGreen, ConCyan, ConBlue, ConBrightBlue, ConMagenta,
}

// Glyph256LUT is GlyphColorLUT for 256 colors: its three levels stay apart in xterm and,
// through the Linux console's 16 colors, Dark stays apart from Normal and Bright. Blue sits
// on the cube's cyan side, where no common console palette turns it near-black.
var Glyph256LUT = [5][3]uint8{
	{color.Cube256(0, 2, 0), color.Cube256(0, 5, 0), color.Cube256(1, 5, 1)}, // Green
	{color.Cube256(1, 2, 3), color.Cube256(1, 3, 5), color.Cube256(2, 3, 5)}, // Blue
	{color.Cube256(3, 0, 0), color.Cube256(5, 0, 0), color.Cube256(5, 1, 1)}, // Red
	{color.Cube256(5, 5, 5), color.Cube256(5, 5, 5), color.Cube256(5, 5, 5)}, // White
	{color.P256Yellow, color.P256Yellow, color.P256Yellow},                   // Gold
}

// Energy-based shield rims
const (
	Shield256Positive = ConBrightYellow
	Shield256Negative = ConMagenta
)

// Lightning256ColorLUT is the background per lightning color type: cyan, red, gold, green, violet
var Lightning256ColorLUT = [5]uint8{ConBrightCyan, ConBrightRed, ConBrightYellow, ConBrightGreen, ConBrightMagenta}

// SpiritBaseOffsets color determines starting point in gradient (0-255) for spirit animation coloring
// Uses existing HeatGradientLUT, progress maps to LUT range based on base color offset
var SpiritBaseOffsets = [8]int{
	0,   // Red
	32,  // Orange
	64,  // Yellow
	96,  // Green
	128, // Cyan
	160, // Blue
	192, // Magenta
	224, // White (wrap to red)
}

// 256-color entity colors
const (
	Missile256Trail           = ConYellow
	Missile256Base            = ConBrightYellow
	SwarmChargeLine256Palette = ConMagenta
	// Loot256Rim is the magenta the Linux console showed its rose rim as, which reads well
	Loot256Rim        = ConBrightMagenta
	Quasar256Rim      = ConWhite
	Bullet256StormRed = ConBrightRed
)

// Palette256RGB returns the xterm RGB of a 256-color index, so an RGB blend can compose
// over a palette cell; indices 0-15 take the VGA system colors
func Palette256RGB(idx uint8) color.RGB {
	switch {
	case idx >= 232:
		v := 8 + 10*(idx-232)
		return color.RGB{R: v, G: v, B: v}
	case idx >= 16:
		r, g, b := color.CubeRGB256(idx)
		return color.RGB{R: cubeLevel(r), G: cubeLevel(g), B: cubeLevel(b)}
	}
	lo, hi := uint8(0), uint8(0xaa)
	if idx >= 8 {
		lo, hi = 0x55, 0xff
	}
	channel := func(bit uint8) uint8 {
		if idx&bit != 0 {
			return hi
		}
		return lo
	}
	return color.RGB{R: channel(1), G: channel(2), B: channel(4)}
}

// cubeLevel maps an xterm color cube coordinate (0-5) to its channel value
func cubeLevel(l uint8) uint8 {
	if l == 0 {
		return 0
	}
	return 55 + 40*l
}
