// @lixen: #focus{vfx[ping,grid]}
// @lixen: #interact{state[ping]}
package components

import (
	"time"
)

type PingComponent struct {
	// Crosshair (Ping) State
	ShowCrosshair  bool
	CrosshairColor ColorClass // Resolves to RGB per player/team

	// Grid (PingGrid) State
	GridActive    bool
	GridRemaining time.Duration // Remaining time in seconds
	GridColor     ColorClass

	// Rendering Hints
	ContextAware bool // Enables dynamic blending (Dark on text / Light on empty)
}