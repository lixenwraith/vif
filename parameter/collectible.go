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