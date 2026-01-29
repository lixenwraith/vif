package visual

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Heat gradient segment thresholds (6 segments, roughly 1/6 each)
const (
	GradientSeg1 = 0.167 // Red to Orange
	GradientSeg2 = 0.333 // Orange to Yellow
	GradientSeg3 = 0.500 // Yellow to Green
	GradientSeg4 = 0.667 // Green to Cyan
	GradientSeg5 = 0.833 // Cyan to Blue
	// Remainder: Blue to Purple/Pink
)

// Heat gradient keyframe colors (rainbow spectrum endpoints)
var (
	GradientDeepRed = terminal.RGB{139, 0, 0}
	GradientOrange  = terminal.RGB{255, 69, 0}
	GradientYellow  = terminal.RGB{255, 215, 0}
	GradientGreen   = terminal.RGB{34, 139, 34}
	GradientCyan    = terminal.RGB{0, 206, 209}
	GradientBlue    = terminal.RGB{65, 105, 225}
	GradientPurple  = terminal.RGB{219, 112, 147}
)

// Rainbow LUT index bounds for readable text backgrounds
// Avoids dark red (0-39) and dark purple (221-255) extremes
const (
	RainbowLUTMin   = 40
	RainbowLUTMax   = 220
	RainbowLUTRange = RainbowLUTMax - RainbowLUTMin // 180
)