package parameter

import (
	"time"
)

// Gold Mechanics
const (
	// GoldDuration is how long the gold sequence remains on screen
	GoldDuration = 10 * time.Second

	// GoldSequenceLength is the number of characters in the gold sequence
	GoldSequenceLength = 10

	// GoldJumpCostPercent is the energy cost to jump to gold
	GoldJumpCostPercent = 10
)

// Nugget System
const (
	// NuggetHeatIncrease is the amount of heat increased by consuming a nugget
	NuggetHeatIncrease = 10

	// NuggetSpawnInterval is the minimum interval between nugget spawns
	NuggetSpawnInterval = 0 * time.Millisecond

	// NuggetMaxAttempts is the maximum number of random placement attempts (occupied cell results in retry)
	NuggetMaxAttempts = 100

	// NuggetJumpCostPercent is the energy cost to jump to a nugget
	NuggetJumpCostPercent = 1

	// NuggetBeaconInterval is the interval between directional cleaner emissions
	NuggetBeaconInterval = 2 * time.Second
)