// @focus: #core { ecs, types } #game { splash, gold }
package components

import (
	"time"
)

// SplashColor defines the semantic color for splash effects
// This decouples components from the render package to avoid cyclic dependencies
type SplashColor uint8

const (
	SplashColorNone SplashColor = iota
	SplashColorNormal
	SplashColorInsert
	SplashColorGreen
	SplashColorBlue
	SplashColorRed
	SplashColorGold
	SplashColorNugget
)

// SplashMode defines the behavior and lifecycle of a splash entity
type SplashMode uint8

const (
	// SplashModeTransient entities automatically expire after Duration
	// Used for input feedback, nuggets, commands
	SplashModeTransient SplashMode = iota

	// SplashModePersistent entities persist until explicitly destroyed via event
	// Used for the Gold Timer
	SplashModePersistent
)

// SplashComponent holds state for splash effects (typing feedback, timers)
// Supports multiple concurrent entities
type SplashComponent struct {
	Content [8]rune     // Content buffer
	Length  int         // Active character count
	Color   SplashColor // Render color
	AnchorX int         // Game-relative X
	AnchorY int         // Game-relative Y

	// Lifecycle & Animation
	Mode       SplashMode    // Transient vs Persistent
	Remaining  time.Duration // Time remaining until expiration (Delta-based)
	Duration   time.Duration // Total initial duration (for progress/animations)
	SequenceID int           // ID for linking to game mechanics (e.g. Gold Sequence)
}