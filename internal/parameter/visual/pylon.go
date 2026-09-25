package visual

import (
	"github.com/lixenwraith/color"
)

// Pylon health color zones (TrueColor)
var (
	// Healthy zone (1.0 - 0.6): Blue
	RgbPylonHealthyBright = color.CeruleanBlue
	RgbPylonHealthyDark   = color.NavyBlue

	// Damaged zone (0.6 - 0.3): Green
	RgbPylonDamagedBright = color.SeaGreen
	RgbPylonDamagedDark   = color.DeepForest

	// Critical zone (0.3 - 0.0): Red
	RgbPylonCriticalBright = color.Cinnabar
	RgbPylonCriticalDark   = color.DarkBurgundy

	// Glow color (constant blue)
	RgbPylonGlow = color.SteelBlue
)

// Pylon 256-color health zones
const (
	Pylon256Healthy  = ConBlue
	Pylon256Damaged  = ConGreen
	Pylon256Critical = ConRed
)

// Pylon glow parameters (reuse storm values for consistency)
const (
	PylonGlowExtend         = 1.5
	PylonGlowIntensityMin   = 0.3
	PylonGlowIntensityMax   = 0.6
	PylonGlowFalloffMult    = 2.0
	PylonGlowOuterDistSqMax = 2.25 // (1.5)^2
)

// Health ratio thresholds
const (
	PylonHealthThresholdDamaged  = 0.6
	PylonHealthThresholdCritical = 0.3
)
