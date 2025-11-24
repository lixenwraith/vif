package constants

import (
	"time"
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

	// CleanerRemovalFlashDuration is how long the removal flash effect lasts in milliseconds
	CleanerRemovalFlashDuration = 150
)
