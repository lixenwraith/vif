package constants

import (
	"time"
)

// TODO: clean up the legacy cleaner constants
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