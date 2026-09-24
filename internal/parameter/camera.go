package parameter

// Camera dead zone configuration
// Dead zone is the inner area where cursor movement doesn't trigger camera scroll
// Margin is the outer area between dead zone and viewport edge
const (
	// CameraDeadZoneMarginX is horizontal margin in cells from viewport edge
	// Cursor entering this margin triggers horizontal camera shift
	CameraDeadZoneMarginX = 30

	// CameraDeadZoneMarginY is vertical margin in cells from viewport edge
	// Cursor entering this margin triggers vertical camera shift
	CameraDeadZoneMarginY = 15

	// CameraPointerMarginX/Y replace the margins while the pointer placed the cursor.
	// A pointer names a screen cell, so a wide margin scrolls the map under a pointer
	// held still and the next report lands further on; a narrow one scrolls only at
	// the edge, by at most its width per report.
	CameraPointerMarginX = 8
	CameraPointerMarginY = 4

	// CameraEnabled controls whether camera following is active
	// When false, camera stays at (0,0) regardless of cursor position
	CameraEnabled = true
)
