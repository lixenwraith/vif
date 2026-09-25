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
	Missile256HostileTrail    = ConRed
	Missile256HostileBase     = ConBrightRed
	SwarmChargeLine256Palette = ConMagenta
	// Loot256Rim is the magenta the Linux console showed its rose rim as, which reads well
	Loot256Rim        = ConBrightMagenta
	Quasar256Rim      = ConWhite
	Bullet256StormRed = ConBrightRed
	Bullet256Player   = ConBrightBlue
)

// Effects the console cannot blend, drawn solid where TrueColor's blend is strong enough to show
const (
	Pulse256Positive  = ConBrightYellow
	Pulse256Negative  = ConBrightMagenta
	Pulse256Hostile   = ConRed
	QuasarZap256Idle  = ConCyan
	QuasarZap256Armed = ConRed
	// Storm256Ring marks the near storm sphere, the one that can be hit
	Storm256Ring       = ConWhite
	Storm256GreenPulse = ConBrightGreen
	Storm256Muzzle     = ConBrightYellow
	Storm256BlueGlow   = ConYellow
	// Effect256Threshold is the least blend alpha a solid 256-color effect cell stands for
	Effect256Threshold = 0.25
)

// Storm256Bodies is each storm sphere's flat body, by StormCircleType
var Storm256Bodies = [3]uint8{ConGreen, ConRed, ConBlue}

// Explosion256 is each explosion type's edge, mid and core color: dust, missile, eye
var Explosion256 = [3][3]uint8{
	{ConBlue, ConCyan, ConBrightCyan},
	{ConRed, ConYellow, ConBrightWhite},
	{ConMagenta, ConBrightMagenta, ConBrightWhite},
}

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
