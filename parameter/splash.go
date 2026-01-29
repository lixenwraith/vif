package parameter

import (
	"time"
)

// Splash Entity
const (
	SplashCharWidth  = 12
	SplashCharHeight = 12
	SplashMaxLength  = 8
	SplashDuration   = 200 * time.Millisecond

	// SplashTimerPadding is the vertical padding between timer and anchor
	SplashTimerPadding = 1

	// SplashTopPadding is adjustment for splash displayed on top/top-right/top-left/right/left of an anchor to account for vertical asymmetry of empty lines above and below splash font (1 top, 2 bottom)
	SplashTopPadding = -1

	// SplashCollisionPadding is the cell padding between different splashes to prevent overcrowding
	SplashCollisionPadding = 2
)