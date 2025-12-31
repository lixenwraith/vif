package component

import (
	"time"
)

type PingComponent struct {
	// Crosshair (Ping) State
	ShowCrosshair bool

	// Grid (PingGrid) State
	GridActive    bool
	GridRemaining time.Duration // Remaining time in seconds

	// Rendering Hints
	ContextAware bool // Enables dynamic blending (Dark on text / Light on empty)
}