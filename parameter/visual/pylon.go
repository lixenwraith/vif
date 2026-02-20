package visual

import "github.com/lixenwraith/vi-fighter/terminal"

// Pylon health color zones (TrueColor)
var (
	// Healthy zone (1.0 - 0.6): Blue
	RgbPylonHealthyBright = terminal.RGB{R: 80, G: 140, B: 220}
	RgbPylonHealthyDark   = terminal.RGB{R: 30, G: 60, B: 120}

	// Damaged zone (0.6 - 0.3): Green
	RgbPylonDamagedBright = terminal.RGB{R: 60, G: 180, B: 80}
	RgbPylonDamagedDark   = terminal.RGB{R: 25, G: 80, B: 35}

	// Critical zone (0.3 - 0.0): Red
	RgbPylonCriticalBright = terminal.RGB{R: 200, G: 60, B: 50}
	RgbPylonCriticalDark   = terminal.RGB{R: 100, G: 25, B: 20}

	// Glow color (constant blue)
	RgbPylonGlow = terminal.RGB{R: 60, G: 100, B: 180}
)

// Pylon 256-color palette indices
const (
	Pylon256Healthy  uint8 = 27  // Blue
	Pylon256Damaged  uint8 = 34  // Green
	Pylon256Critical uint8 = 160 // Red
)

// Pylon basic color (8/16 color terminals)
const (
	PylonBasicHealthy  uint8 = 4 // Blue
	PylonBasicDamaged  uint8 = 2 // Green
	PylonBasicCritical uint8 = 1 // Red
)

// Pylon glow parameters (reuse storm values for consistency)
const (
	PylonGlowExtendFloat    = 1.5
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