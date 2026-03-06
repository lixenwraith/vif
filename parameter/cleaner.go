package parameter

import "github.com/lixenwraith/vi-fighter/vmath"

// Cleaner physics
var (
	CleanerBaseHorizontalSpeed = vmath.FromFloat(CleanerBaseHorizontalSpeedFloat)
	CleanerBaseVerticalSpeed   = vmath.FromFloat(CleanerBaseVerticalSpeedFloat)
	CleanerTrailLenFixed       = vmath.FromInt(CleanerTrailLength)
)

// Cleaner Entity
const (
	// CleanerBaseHorizontalSpeed
	CleanerBaseHorizontalSpeedFloat = 80.0
	// CleanerBaseVerticalSpeed
	CleanerBaseVerticalSpeedFloat = 40.0

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10
)
