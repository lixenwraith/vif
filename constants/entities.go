// @focus: #constants { entities }
package constants

import "time"

// --- Cleaner Entity ---
const (
	// CleanerChar is the character used to render the cleaner block
	CleanerChar = '█'

	// CleanerAnimationDuration is the total time for a cleaner to sweep across the screen
	CleanerAnimationDuration = 1.0 * time.Second

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10

	// CleanerDeduplicationWindow is the number of frames to prevent duplicate spawns
	CleanerDeduplicationWindow = 30
)

// --- Drain Entity ---
const (
	// DrainChar is the character used to render the drain entity (╬ - Unicode U+256C)
	DrainChar = '╬'

	// DrainMoveInterval is the duration between drain movement updates
	DrainMoveInterval = 1000 * time.Millisecond

	// DrainEnergyDrainInterval is the duration between energy drain ticks
	DrainEnergyDrainInterval = 1000 * time.Millisecond

	// DrainEnergyDrainAmount is the amount of energy drained per tick
	DrainEnergyDrainAmount = 10
)

// --- Decay System ---
const (
	// DecayRowAnimationDurationMs is the time per row during decay animation
	DecayRowAnimationDurationMs = 100
	DecayRowAnimationDuration   = DecayRowAnimationDurationMs * time.Millisecond

	// DecayIntervalBaseSeconds is the base decay interval at zero heat
	DecayIntervalBaseSeconds = 60
	// DecayIntervalRangeSeconds is the range of decay interval affected by heat
	DecayIntervalRangeSeconds = 50
	// DecayIntervalMinSeconds is the minimum decay interval at max heat
	DecayIntervalMinSeconds = DecayIntervalBaseSeconds - DecayIntervalRangeSeconds

	// Decay Speed Limits (rows per second)
	DecayMinSpeed = 5.0
	DecayMaxSpeed = 15.0

	// Matrix-style Effect Constants
	DecayChangeChance          = 0.4
	DecayMinRowsBetweenChanges = 1
)

// --- Materialization Effect ---
const (
	// MaterializeChar is the character used for spawn animation blocks
	MaterializeChar = '█'

	// MaterializeAnimationDuration is the time for spawners to converge
	MaterializeAnimationDuration = 1 * time.Second

	// MaterializeTrailLength is the number of trail positions for fade effect
	MaterializeTrailLength = 8
)

// --- Shield Entity ---
const (
	ShieldRadiusX    = 10.5
	ShieldRadiusY    = 5.5
	ShieldMaxOpacity = 0.3
)

// --- Splash Entity ---
const (
	SplashCharWidth   = 16
	SplashCharHeight  = 12
	SplashCharSpacing = 1
	SplashMaxLength   = 8
	SplashDuration    = 1 * time.Second

	// SplashTimerPadding is the vertical padding between gold timer and sequence
	SplashTimerPadding = 2
)

// --- Global Visual Effects ---
const (
	// DestructionFlashDuration is how long the destruction flash effect lasts in milliseconds
	// Used for drain collision, decay terminal, cleaner sweep
	DestructionFlashDuration = 500 * time.Millisecond
)