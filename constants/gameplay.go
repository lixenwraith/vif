package constants

import "time"

// Heat & Score System
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

	// GoldInitialSpawnDelay is the delay before spawning the first gold sequence
	GoldInitialSpawnDelay = 150 * time.Millisecond
)

// Nugget Spawning Mechanics
const (
	NuggetSpawnIntervalSeconds = 5
	NuggetMaxAttempts          = 100
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
