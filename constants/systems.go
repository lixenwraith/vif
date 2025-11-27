package constants

import (
	"time"
)

// Energy System
const (
	// ErrorBlinkTimeout is the duration for error cursor flash
	ErrorBlinkTimeout = 200 * time.Millisecond

	// EnergyBlinkTimeout is the duration for energy blink
	EnergyBlinkTimeout = 200 * time.Millisecond
)

// Gold System
const (
	// GoldDuration is how long the gold sequence remains on screen
	GoldDuration = 10 * time.Second

	// GoldSequenceLength is the number of characters in the gold sequence
	GoldSequenceLength = 10

	// GoldInitialSpawnDelay is the delay before spawning the first gold sequence
	GoldInitialSpawnDelay = 150 * time.Millisecond
)

// Decay System
const (
	// DecayRowAnimationDurationMs is the time per row during decay animation
	DecayRowAnimationDurationMs = 100

	// DecayRowAnimationDuration is the time per row during decay animation
	DecayRowAnimationDuration = DecayRowAnimationDurationMs * time.Millisecond

	// DecayIntervalBaseSeconds is the base decay interval at zero heat
	DecayIntervalBaseSeconds = 60

	// DecayIntervalRangeSeconds is the range of decay interval affected by heat
	DecayIntervalRangeSeconds = 50

	// DecayIntervalMinSeconds is the minimum decay interval at max heat
	DecayIntervalMinSeconds = DecayIntervalBaseSeconds - DecayIntervalRangeSeconds // 10 seconds

	// DecayMinSpeed is the minimum falling speed in rows per second
	DecayMinSpeed = 5.0

	// DecayMaxSpeed is the maximum falling speed in rows per second
	// One falling entity is spawned per column with random speeds between min and max
	DecayMaxSpeed = 15.0

	// DecayChangeChance is the probability (0.0-1.0) that a falling character
	// will change to a new random character when eligible (Matrix-style effect)
	DecayChangeChance = 0.4

	// DecayMinRowsBetweenChanges is the minimum number of rows that must pass
	// before a falling character is eligible to change again
	DecayMinRowsBetweenChanges = 1
)

// Drain System
const (
	// DrainChar is the character used to render the drain entity (╬ - Unicode U+256C)
	DrainChar = '╬'

	// DrainMoveIntervalMs is the interval between drain movement updates (in milliseconds)
	DrainMoveIntervalMs = 1000

	// DrainMoveInterval is the duration between drain movement updates
	DrainMoveInterval = DrainMoveIntervalMs * time.Millisecond

	// DrainEnergyDrainIntervalMs is the interval between energy drain ticks (in milliseconds)
	DrainEnergyDrainIntervalMs = 1000

	// DrainEnergyDrainInterval is the duration between energy drain ticks
	DrainEnergyDrainInterval = DrainEnergyDrainIntervalMs * time.Millisecond

	// DrainEnergyDrainAmount is the amount of energy drained per tick (10 points)
	DrainEnergyDrainAmount = 10
)