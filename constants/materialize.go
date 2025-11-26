package constants

import "time"

// Materialize System Constants
const (
	// MaterializeAnimationDuration is the time for spawners to converge (1 second)
	MaterializeAnimationDuration = 1 * time.Second

	// MaterializeTrailLength is the number of trail positions for fade effect
	MaterializeTrailLength = 8

	// MaterializeChar is the character used for spawn animation blocks
	MaterializeChar = '█'
)