package components

import "github.com/lixenwraith/vi-fighter/terminal"

// SplashMode defines the lifecycle behavior of a splash entity
type SplashMode uint8

const (
	// SplashModeTransient entities automatically expire after Duration
	SplashModeTransient SplashMode = iota
	// SplashModePersistent entities persist until explicitly destroyed
	SplashModePersistent
)

// SplashComponent holds state for splash effects (typing feedback, timers)
// Supports multiple concurrent entities
type SplashComponent struct {
	Content [8]rune // Content buffer
	Length  int     // Active character count
	// TODO: modify to ColorClass
	Color   terminal.RGB // Render color
	AnchorX int          // Game-relative X
	AnchorY int          // Game-relative Y

	// Lifecycle & Animation
	Mode      SplashMode // Transient vs Persistent
	StartNano int64      // GameTime at start of animation (for opacity calc)
	Duration  int64      // Duration in nanoseconds (TTL or Pulse cycle)
}