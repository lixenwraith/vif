package visual

// Pylon health color zones (TrueColor)
var (
	// Healthy zone (1.0 - 0.6): Blue
	RgbPylonHealthyBright = CeruleanBlue
	RgbPylonHealthyDark   = NavyBlue

	// Damaged zone (0.6 - 0.3): Green
	RgbPylonDamagedBright = SeaGreen
	RgbPylonDamagedDark   = DeepForest

	// Critical zone (0.3 - 0.0): Red
	RgbPylonCriticalBright = Cinnabar
	RgbPylonCriticalDark   = DarkBurgundy

	// Glow color (constant blue)
	RgbPylonGlow = SteelBlue
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