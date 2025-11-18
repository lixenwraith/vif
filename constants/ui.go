package constants

import "time"

// UI Layout Constants
const (
	// HeatBarIndicatorWidth is the width reserved for the heat value indicator (right side)
	HeatBarIndicatorWidth = 6
)

// UI Timing Constants (in milliseconds)
const (
	// ErrorCursorTimeoutMs is how long the error cursor flashes
	ErrorCursorTimeoutMs = 200

	// ScoreBlinkTimeoutMs is how long the score blinks after scoring
	ScoreBlinkTimeoutMs = 300

	// ErrorCursorTimeout is the duration for error cursor flash
	ErrorCursorTimeout = ErrorCursorTimeoutMs * time.Millisecond

	// ScoreBlinkTimeout is the duration for score blink
	ScoreBlinkTimeout = ScoreBlinkTimeoutMs * time.Millisecond
)

// Game Timing Constants
const (
	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond
)

// Gold Sequence Constants
const (
	// GoldSequenceDuration is how long the gold sequence remains on screen
	GoldSequenceDuration = 10 * time.Second

	// GoldSequenceLength is the number of characters in the gold sequence
	GoldSequenceLength = 10
)

// Decay System Constants
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

	// FallingDecayMinSpeed is the minimum falling speed in rows per second
	FallingDecayMinSpeed = 5.0

	// FallingDecayMaxSpeed is the maximum falling speed in rows per second
	// Note: One falling entity is spawned per column with random speeds between min and max
	FallingDecayMaxSpeed = 15.0

	// FallingDecayChangeChance is the probability (0.0-1.0) that a falling character
	// will change to a new random character when eligible (Matrix-style effect)
	FallingDecayChangeChance = 0.4

	// FallingDecayMinRowsBetweenChanges is the minimum number of rows that must pass
	// before a falling character is eligible to change again
	FallingDecayMinRowsBetweenChanges = 1
)

// Cleaner System Constants
const (
	// CleanerAnimationDuration is the total time for a cleaner to sweep across the screen
	CleanerAnimationDuration = 1.0 * time.Second

	// CleanerTrailFadeTime is how long the trail takes to fade from bright yellow to transparent
	CleanerTrailFadeTime = 0.3 // seconds (as float64 for interpolation)

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10

	// CleanerFPS is the target frame rate for smooth cleaner animation
	CleanerFPS = 60

	// CleanerChar is the character used to render the cleaner block
	CleanerChar = '█'

	// RemovalFlashDuration is how long the removal flash effect lasts in milliseconds
	RemovalFlashDuration = 150
)
