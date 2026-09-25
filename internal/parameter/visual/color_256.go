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
