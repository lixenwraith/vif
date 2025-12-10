// @focus: #constants { gameplay }
package constants

import "time"

// Heat System
const (
	// MaxHeat is the maximum value for the heat meter (100%)
	MaxHeat = 100

	// NuggetHeatIncrease is the amount of heat increased by consuming a nugget
	NuggetHeatIncrease = 10
)

// Energy System
const (
	// EnergyBlinkTimeout is the duration for energy blink
	EnergyBlinkTimeout = 200 * time.Millisecond

	// ErrorBlinkTimeout is the duration for error cursor flash
	ErrorBlinkTimeout = 200 * time.Millisecond
)

// Boost Mechanics
const (
	// BoostBaseDuration is the initial duration when boost is activated via command
	BoostBaseDuration = 10 * time.Second

	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond
)

// Gold Sequence Mechanics
const (
	// GoldDuration is how long the gold sequence remains on screen
	GoldDuration = 10 * time.Second

	// GoldSequenceLength is the number of characters in the gold sequence
	GoldSequenceLength = 10
)

// Nugget Spawning Mechanics
const (
	// NuggetSpawnIntervalSeconds is the minimum interval between nugget spawns
	NuggetSpawnIntervalSeconds = 5

	// NuggetMaxAttempts is the maximum number of random placement attempts (occupied cell results in retry)
	NuggetMaxAttempts = 100

	// NuggetJumpCost is the energy cost to jump to a nugget
	NuggetJumpCost = 10
)

// Character Spawn Logic
const (
	SpawnIntervalMs         = 2000
	MinBlockLines           = 3
	MaxBlockLines           = 15
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

// Drain System
const (
	// DrainMaxCount is the maximum number of drain entities (at 100% heat)
	DrainMaxCount = 10

	// DrainShieldEnergyDrainAmount is energy cost per tick per drain inside shield
	DrainShieldEnergyDrainAmount = 100

	// DrainHeatReductionAmount is heat penalty when drain hits cursor without shield
	DrainHeatReductionAmount = 10

	// DrainSpawnOffsetMax is the maximum random offset from cursor position (±N)
	DrainSpawnOffsetMax = 10

	// DrainSpawnStaggerTicks is game ticks between staggered spawns
	// Set to 0 for simultaneous spawning
	DrainSpawnStaggerTicks = 4
)

// Shield Defense Costs
const (
	// ShieldPassiveDrainAmount is energy cost per second while shield is active
	ShieldPassiveDrainAmount = 1

	// ShieldPassiveDrainInterval is the interval for passive shield drain
	ShieldPassiveDrainInterval = 1 * time.Second
)

// Spawn Rate Management
const (
	// SpawnDensityLowThreshold is the entity density below which spawn rate increases
	SpawnDensityLowThreshold = 0.3 // Below: 2x spawn rate

	// SpawnDensityHighThreshold is the entity density above which spawn rate decreases
	SpawnDensityHighThreshold = 0.7 // Above: 0.5x spawn rate

	// Spawn rate multipliers
	SpawnRateFast   = 2.0 // Used when density < low threshold
	SpawnRateNormal = 1.0 // Used when density is between thresholds
	SpawnRateSlow   = 0.5 // Used when density > high threshold
)

// Position Finding
const (
	// DrainSpawnMaxRetries is the maximum number of retries for finding valid drain spawn position
	DrainSpawnMaxRetries = 10

	// GoldSpawnMaxAttempts is the maximum number of attempts to find valid gold sequence position
	GoldSpawnMaxAttempts = 100
)