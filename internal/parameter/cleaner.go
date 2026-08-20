package parameter

// Cleaner Entity
const (
	// CleanerBaseHorizontalSpeed
	CleanerBaseHorizontalSpeed = 80.0
	// CleanerBaseVerticalSpeed
	CleanerBaseVerticalSpeed = 40.0

	// CleanerTrailLength is the number of previous positions tracked for the fade trail effect
	CleanerTrailLength = 10.0

	// CleanerTrailHeadIntensity and CleanerTrailTailIntensity bound the
	// background-only trail brightness. Max blending keeps overlapping automatic
	// fire steady instead of accumulating into a flash.
	CleanerTrailHeadIntensity = 0.9
	CleanerTrailTailIntensity = 0.12
)
