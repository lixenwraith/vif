package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// OrbComponent tracks orbital state for buff visualization orbs
type OrbComponent struct {
	BuffType    BuffType    // Which buff this orb represents
	OwnerEntity core.Entity // Cursor entity that owns this orb

	// Orbital state (Q32.32)
	OrbitAngle   int64 // Current angle, Scale = full rotation (2π)
	OrbitRadiusX int64 // Horizontal radius
	OrbitRadiusY int64 // Vertical radius
	OrbitSpeed   int64 // Rotations per second, Scale = 1 rot/sec

	// Redistribution animation
	StartAngle            int64         // Angle at redistribution start
	TargetAngle           int64         // Target angle after redistribution
	RedistributeRemaining time.Duration // 0 = not redistributing

	// Fire flash effect
	FlashRemaining time.Duration
}