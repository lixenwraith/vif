package visual

// Heat256LUT contains xterm 256-palette indices for 10 heat segments
// Progression: deep red → orange → yellow → green → cyan → blue → purple
// Indices from 6×6×6 color cube: index = 16 + 36*r + 6*g + b where r,g,b ∈ [0,5]
var Heat256LUT = [10]uint8{
	196, // 0-10%:   Red (5,0,0)
	202, // 10-20%:  Red-orange (5,1,0)
	208, // 20-30%:  Orange (5,2,0)
	220, // 30-40%:  Yellow-orange (5,4,0)
	154, // 40-50%:  Yellow-green (3,5,0)
	46,  // 50-60%:  Green (0,5,0)
	51,  // 60-70%:  Cyan (0,5,5)
	33,  // 70-80%:  Blue (0,2,5)
	63,  // 80-90%:  Blue-purple (1,1,5)
	129, // 90-100%: Purple (3,0,5)
}

// 256-color palette indices for energy-based shield colors
const (
	Shield256Positive = 226 // Bright yellow (matches RgbCleanerBasePositive)
	Shield256Negative = 134 // Violet (matches RgbCleanerBaseNegative)
)

// Lightning256ColorLUT is 256-color fixed palette indices per lightning color type
// Uses xterm 256-palette for consistent appearance without blending
var Lightning256ColorLUT = [5]uint8{
	51,  // Cyan (0,5,5) - bright cyan
	196, // Red (5,0,0) - bright red
	220, // Gold (5,4,0) - yellow-orange
	46,  // Green (0,5,0) - bright green
	129, // Purple (3,0,5) - medium purple
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