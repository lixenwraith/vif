package constant

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

// Gold Mechanics
const (
	// GoldDuration is how long the gold sequence remains on screen
	GoldDuration = 10 * time.Second

	// GoldSequenceLength is the number of characters in the gold sequence
	GoldSequenceLength = 10

	// GoldJumpCost is the energy cost to jump to gold
	GoldJumpCost = 1000
)

// Nugget System
const (
	// NuggetHeatIncrease is the amount of heat increased by consuming a nugget
	NuggetHeatIncrease = 10

	// NuggetSpawnInterval is the minimum interval between nugget spawns
	NuggetSpawnInterval = 0 * time.Millisecond

	// NuggetMaxAttempts is the maximum number of random placement attempts (occupied cell results in retry)
	NuggetMaxAttempts = 100

	// NuggetJumpCost is the energy cost to jump to a nugget
	NuggetJumpCost = 100

	// NuggetOverloadCount is the number of nuggets that are taken at max heat to trigger nugget overload
	NuggetOverloadCount = 10
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
	// ShieldPassiveEnergyPercentDrain is the energy percentage of total per second while shield is active
	ShieldPassiveEnergyPercentDrain = 1

	// ShieldPassiveDrainInterval is the interval for passive shield drain
	ShieldPassiveDrainInterval = 1 * time.Second
)

// Vampire Drain
const (
	// VampireDrainEnergyValue is the amount of energy absorbed energy on hit
	VampireDrainEnergyValue = 100
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

// Buff Cooldowns
const (
	BuffCooldownRod      = 500 * time.Millisecond
	BuffCooldownLauncher = 1000 * time.Millisecond
	BuffCooldownChain    = 2000 * time.Millisecond
)

// Combat
const (
	CombatKnockbackImmunityInterval = 250 * time.Millisecond
)