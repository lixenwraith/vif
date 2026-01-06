package component

import "time"

// ExplosionComponent represents an expanding visual explosion effect
// Logic (glyph transformation) is instant; this component drives animation only
type ExplosionComponent struct {
	// Center position (grid coordinates)
	CenterX, CenterY int

	// Q32.32 radii for visual expansion
	MaxRadius     int64 // Target visual radius
	CurrentRadius int64 // Expanding radius (0 → MaxRadius)

	// Timing
	Duration time.Duration
	Age      time.Duration
}