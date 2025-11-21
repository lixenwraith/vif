package constants

import "time"

// UI Layout Constants
const (
	// ModeIndicatorWidth is the consistent width for all mode indicators
	ModeIndicatorWidth = 10

	// Mode indicator text (all padded to ModeIndicatorWidth)
	ModeTextNormal  = " NORMAL  "
	ModeTextInsert  = " INSERT  "
	ModeTextSearch  = " SEARCH  "
	ModeTextCommand = " COMMAND "
)

// UI Timing Constants (in milliseconds)
const (
	// ErrorCursorTimeoutMs is how long the error cursor flashes
	ErrorCursorTimeoutMs = 200

	// ScoreBlinkTimeoutMs is how long the score blinks after scoring
	ScoreBlinkTimeoutMs = 200

	// ErrorCursorTimeout is the duration for error cursor flash
	ErrorCursorTimeout = ErrorCursorTimeoutMs * time.Millisecond

	// ScoreBlinkTimeout is the duration for score blink
	ScoreBlinkTimeout = ScoreBlinkTimeoutMs * time.Millisecond
)

// Game Timing Constants
const (
	// BoostExtensionDuration is how long each matching color character extends the boost
	BoostExtensionDuration = 500 * time.Millisecond

	// BoostBaseDuration is the initial duration when boost is activated via command
	BoostBaseDuration = 10 * time.Second

	// CommandStatusMessageTimeout is how long command status messages are displayed
	CommandStatusMessageTimeout = 2 * time.Second
)

// Gold Constants
const (
	// GoldDuration is how long the gold sequence remains on screen
	GoldDuration = 10 * time.Second

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

// Drain System Constants
const (
	// DrainChar is the character used to render the drain entity (╬ - Unicode U+256C)
	DrainChar = '╬'

	// DrainMoveIntervalMs is the interval between drain movement updates (in milliseconds)
	DrainMoveIntervalMs = 1000

	// DrainMoveInterval is the duration between drain movement updates
	DrainMoveInterval = DrainMoveIntervalMs * time.Millisecond

	// DrainScoreDrainIntervalMs is the interval between score drain ticks (in milliseconds)
	DrainScoreDrainIntervalMs = 1000

	// DrainScoreDrainInterval is the duration between score drain ticks
	DrainScoreDrainInterval = DrainScoreDrainIntervalMs * time.Millisecond

	// DrainScoreDrainAmount is the amount of score drained per tick (10 points)
	DrainScoreDrainAmount = 10
)

// Cleaner System Constants (legacy - use CleanerConfig for new code)
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

// TrailFadeCurve defines the interpolation method for trail opacity
type TrailFadeCurve int

const (
	// TrailFadeLinear uses linear interpolation for trail fade
	TrailFadeLinear TrailFadeCurve = iota
	// TrailFadeExponential uses exponential interpolation for trail fade
	TrailFadeExponential
)

// CleanerConfig contains all configurable parameters for the Cleaner system
type CleanerConfig struct {
	// AnimationDuration is the total time for a cleaner to sweep across the screen
	AnimationDuration time.Duration

	// Speed is the cleaner movement speed in characters per second
	// If set to 0, speed is calculated from AnimationDuration and screen width
	Speed float64

	// TrailLength is the number of previous positions tracked for the fade trail effect
	TrailLength int

	// TrailFadeTime is how long the trail takes to fade from bright yellow to transparent (seconds)
	TrailFadeTime float64

	// TrailFadeCurve defines the interpolation method for trail opacity (linear or exponential)
	FadeCurve TrailFadeCurve

	// MaxConcurrentCleaners limits the number of cleaners that can be active simultaneously
	// Set to 0 for unlimited cleaners
	MaxConcurrentCleaners int

	// ScanInterval is the interval between periodic scans for Red characters
	// Set to 0 to disable periodic scanning (cleaners only trigger on gold sequence completion)
	ScanInterval time.Duration

	// FPS is the target frame rate for smooth cleaner animation
	FPS int

	// Char is the character used to render the cleaner block
	Char rune

	// FlashDuration is how long the removal flash effect lasts in milliseconds
	FlashDuration int
}

// DefaultCleanerConfig returns the default cleaner configuration
func DefaultCleanerConfig() CleanerConfig {
	return CleanerConfig{
		AnimationDuration:     CleanerAnimationDuration,
		Speed:                 0, // Auto-calculate from AnimationDuration
		TrailLength:           CleanerTrailLength,
		TrailFadeTime:         CleanerTrailFadeTime,
		FadeCurve:             TrailFadeLinear,
		MaxConcurrentCleaners: 0, // Unlimited
		ScanInterval:          0, // No periodic scanning
		FPS:                   CleanerFPS,
		Char:                  CleanerChar,
		FlashDuration:         RemovalFlashDuration,
	}
}