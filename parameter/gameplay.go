package parameter

import "time"

// Heat System
const (
	// HeatMax is the maximum value for the heat meter (100%)
	HeatMax = 100

	// HeatMaxOverheat is the maximum overheat to trigger overheat event
	HeatMaxOverheat = HeatMax // currently double heat amount

	// HeatTypingErrorPenalty is the heat penalty when wrong character typed in insert mode
	HeatTypingErrorPenalty = 10

	// HeatBurstFlashDuration is the time burst flash indicator is shown on heat bar
	HeatBurstFlashDuration = 150 * time.Millisecond
)

// Energy System
const (
	// EnergyBlinkTimeout is the duration for energy blink
	EnergyBlinkTimeout = 200 * time.Millisecond

	// ErrorBlinkTimeout is the duration for error cursor flash
	ErrorBlinkTimeout = 200 * time.Millisecond
)

// Glyph Energy
const (
	EnergyBaseBlue  = 2
	EnergyBaseGreen = 1
	EnergyBaseRed   = -2
)

// Boost Mechanics
const (
	// BoostBaseDuration is the initial duration when boost is activated
	BoostBaseDuration = 500 * time.Millisecond

	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond
)

// God Mode
const (
	GodEnergyAmount = 100_000_000_000
)

// Character Spawn Logic
const (
	SpawnIntervalMs         = 1000
	MinBlockLines           = 2
	MaxBlockLines           = 5
	MaxPlacementTries       = 3
	MinIndentChange         = 2
	ContentRefreshThreshold = 0.8
)

// Spawn Exclusion Zones
const (
	// CursorExclusionX is horizontal distance from cursor that blocks spawn
	CursorExclusionX = 5
	// CursorExclusionY is vertical distance from cursor that blocks spawn
	CursorExclusionY = 3
)

// Spawn Rate Management
const (
	// SpawnDensityLowThreshold is the entity density below which spawn rate increases
	SpawnDensityLowThreshold = 0.1 // Below: 2x spawn rate

	// SpawnDensityHighThreshold is the entity density above which spawn rate decreases
	SpawnDensityHighThreshold = 0.25 // Above: 0.5x spawn rate

	// Spawn rate multipliers
	SpawnRateFast   = 2.0  // Used when density < low threshold
	SpawnRateNormal = 1.0  // Used when density is between thresholds
	SpawnRateSlow   = 0.02 // Used when density > high threshold
)

// Positions Finding
const (
	// DrainSpawnMaxRetries is the maximum number of retries for finding valid drain spawn position
	DrainSpawnMaxRetries = 10

	// GoldSpawnMaxAttempts is the maximum number of attempts to find valid gold sequence position
	GoldSpawnMaxAttempts = 100
)