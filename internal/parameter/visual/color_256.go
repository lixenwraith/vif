package visual

import "github.com/lixenwraith/color"

// Heat256LUT contains xterm 256-palette indices for 10 heat segments
// Progression: deep red → orange → yellow → green → cyan → blue → purple
var Heat256LUT = [10]uint8{
	color.P256Red,         // 0-10%
	color.P256RedOrange,   // 10-20%
	color.P256Orange,      // 20-30%
	color.P256Gold,        // 30-40%
	color.P256YellowGreen, // 40-50%
	color.P256Green,       // 50-60%
	color.P256Cyan,        // 60-70%
	color.P256CobaltBlue,  // 70-80%
	color.P256Indigo,      // 80-90%
	color.P256Purple,      // 90-100%
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

// 256-color palette indices for energy-based shield colors
const (
	Shield256Positive = color.P256Yellow // Bright yellow
	Shield256Negative = color.P256Violet // Violet
)

// Lightning256ColorLUT is 256-color fixed palette indices per lightning color type
var Lightning256ColorLUT = [5]uint8{
	color.P256Cyan,   // Bright cyan
	color.P256Red,    // Bright red
	color.P256Gold,   // Yellow-orange
	color.P256Green,  // Bright green
	color.P256Purple, // Medium purple
}

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

// 256-colors palette indices
const (
	// Missile
	Missile256Trail = color.P256Amber  // (5,3,0)
	Missile256Base  = color.P256Orange // (5,2,0)

	// Swarm charge line
	SwarmChargeLine256Palette = color.P256Orchid // (4,2,4)

	// Loot shield
	Loot256Rim = color.P256Rose // (5,0,2)

	Bullet256StormRed = color.P256Red // (5,0,0)
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
