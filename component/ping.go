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
}