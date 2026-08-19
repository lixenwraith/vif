package component

import (
	"time"
)

type PingComponent struct {
	// Crosshair (Ping)
	ShowCrosshair bool

	// Grid (PingGrid)
	GridActive    bool
	GridRemaining time.Duration // Remaining time in seconds

	// Ping bounds: half-extents around the cursor cell, active in visual mode with a live shield
	BoundsRadiusX int
	BoundsRadiusY int
	BoundsActive  bool
}
