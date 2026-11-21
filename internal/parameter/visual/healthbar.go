package visual

import "github.com/lixenwraith/color"

// HealthBarPosition defines bar placement relative to entity
type HealthBarPosition uint8

const (
	HealthBarAbove HealthBarPosition = iota
	HealthBarBelow
	HealthBarLeft
	HealthBarRight
)

// Health bar configuration
const (
	HealthBarEnabled      = true
	HealthBarPosDefault   = HealthBarAbove
	HealthBarProportional = true // Length shrinks with HP for composites
	HealthBarMinLength    = 1    // Never shrink below 1 char
)

// Health bar character
const (
	HealthBarChar = '■' // ASCII fallback for 256-color
)

// A body as large as the view pins its bar to the view's top row, across
// HealthBarPinnedShare of its width, on a backing HealthBarPinnedBacking darker than
// what it stands over, the hit points lost dotted with HealthBarLostChar
const (
	HealthBarPinnedShare   = 1.0 / 3
	HealthBarPinnedBacking = 0.75
	HealthBarLostChar      = '·'
)

var RgbHealthBarLost = color.RGB{R: 90, G: 40, B: 40}

const HealthBar256Lost = ConGray

// Health256LUT maps health percentage segments to console colors, 0 critical to 4 full:
// red, orange, yellow, yellow-green, green
var Health256LUT = [5]uint8{ConBrightRed, ConYellow, ConBrightYellow, ConGreen, ConBrightGreen}

// HealthGradientRange defines LUT index range for health color mapping
// Maps health ratio [0.0, 1.0] to HeatGradientLUT indices [Min, Max]
const (
	HealthLUTMin = 30  // Deep red (low health)
	HealthLUTMax = 150 // Green (full health)
)

// MaskHealthBar is render mask for health bar layer
const MaskHealthBar uint8 = 0x40
