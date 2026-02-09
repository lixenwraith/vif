package parameter

// Camera dead zone configuration
// Dead zone is the inner area where cursor movement doesn't trigger camera scroll
// Margin is the outer area between dead zone and viewport edge
const (
	// CameraDeadZoneMarginX is horizontal margin in cells from viewport edge
	// Cursor entering this margin triggers horizontal camera shift
	CameraDeadZoneMarginX = 12

	// CameraDeadZoneMarginY is vertical margin in cells from viewport edge
	// Cursor entering this margin triggers vertical camera shift
	CameraDeadZoneMarginY = 6

	// CameraEnabled controls whether camera following is active
	// When false, camera stays at (0,0) regardless of cursor position
	CameraEnabled = true
)