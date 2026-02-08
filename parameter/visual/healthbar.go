package visual

// TODO: move these to parameters
// HealthBarPosition defines bar placement relative to entity
type HealthBarPosition uint8

const (
	HealthBarAbove HealthBarPosition = iota
	HealthBarBelow
	HealthBarLeft
	HealthBarRight
	HealthBarPositionCount
)

// Health bar configuration
const (
	HealthBarEnabled      = true
	HealthBarPosDefault   = HealthBarAbove
	HealthBarProportional = true // Length shrinks with HP for composites
	HealthBarMinLength    = 1    // Never shrink below 1 char
)

// Health bar characters
const (
	HealthBarCharAbove    = '▄' // Lower half-block (renders at bottom of cell above entity)
	HealthBarCharBelow    = '▀' // Upper half-block
	HealthBarCharLeft     = '▐' // Right half-block (renders at right edge, adjacent to entity)
	HealthBarCharRight    = '▌' // Left half-block
	HealthBarCharFallback = '■' // ASCII fallback for 256-color
)

// Health256LUT maps health percentage segments to xterm-256 palette indices
// Index: 0=critical, 4=full health
var Health256LUT = [5]uint8{
	196, // 0-19% - Red
	208, // 20-39% - Orange
	226, // 40-59% - Yellow
	190, // 60-79% - Yellow-Green
	46,  // 80-100% - Green
}

// HealthGradientRange defines LUT index range for health color mapping
// Maps health ratio [0.0, 1.0] to HeatGradientLUT indices [Min, Max]
const (
	HealthLUTMin = 30  // Deep red (low health)
	HealthLUTMax = 150 // Green (full health)
)

// MaskHealthBar is render mask for health bar layer
const MaskHealthBar uint8 = 0x40