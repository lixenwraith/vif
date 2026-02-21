package visual

// Heat256LUT contains xterm 256-palette indices for 10 heat segments
// Progression: deep red → orange → yellow → green → cyan → blue → purple
var Heat256LUT = [10]uint8{
	P256Red,         // 0-10%
	P256RedOrange,   // 10-20%
	P256Orange,      // 20-30%
	P256Gold,        // 30-40%
	P256YellowGreen, // 40-50%
	P256Green,       // 50-60%
	P256Cyan,        // 60-70%
	P256CobaltBlue,  // 70-80%
	P256Indigo,      // 80-90%
	P256Purple,      // 90-100%
}

// 256-color palette indices for energy-based shield colors
const (
	Shield256Positive = P256Yellow // Bright yellow
	Shield256Negative = P256Violet // Violet
)

// Lightning256ColorLUT is 256-color fixed palette indices per lightning color type
var Lightning256ColorLUT = [5]uint8{
	P256Cyan,   // Bright cyan
	P256Red,    // Bright red
	P256Gold,   // Yellow-orange
	P256Green,  // Bright green
	P256Purple, // Medium purple
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
	Missile256Trail  = P256Amber  // (5,3,0)
	Missile256Body   = P256Gold   // (5,4,0)
	Missile256Seeker = P256Orange // (5,2,0)

	// Swarm charge line
	SwarmChargeLine256Palette = P256Orchid // (4,2,4)

	// Wall fallback
	Wall256PaletteDefault = P256Gray // Grayscale step 8

	// Loot shield
	Loot256Rim = P256Rose // (5,0,2)

	// Storm rendering palette indices
	Storm256Bright = P256LightCyan // (1,5,5)
	Storm256Normal = P256Teal      // (0,4,4)
	Storm256Dark   = P256DeepTeal  // (0,1,1)

	Bullet256StormRed = P256Red // (5,0,0)
)