package parameter

// The camera's edge is [play] camera_edge, a percent of the screen's size kept
// between the cursor and every side of the view; ConfigResource.EdgeCells turns it
// into cells
const (
	// CameraEdgeMost is the widest edge in percent, where every move moves the view
	CameraEdgeMost = 50

	// CameraEdgeStep is the percent one step of the Settings row moves the edge
	CameraEdgeStep = 5

	// CameraEnabled controls whether camera following is active
	// When false, camera stays at (0,0) regardless of cursor position
	CameraEnabled = true

	// ReplayPanStep is the cells a replay's pan key shifts the presented game area
	ReplayPanStep = 4
)
