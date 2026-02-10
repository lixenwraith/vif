package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// OrbComponent tracks orbital state for weapon visualization orbs
type OrbComponent struct {
	WeaponType  WeaponType
	OwnerEntity core.Entity

	// Current angle on orbit (Q32.32, Scale = full rotation)
	OrbitAngle int64

	// Assigned target angle from distribution (Q32.32)
	TargetAngle int64

	// Orbit parameters (Q32.32)
	OrbitRadiusX int64
	OrbitRadiusY int64
	OrbitSpeed   int64 // Rotation speed when orbiting freely

	// Animation state
	RedistributeRemaining time.Duration
	StartAngle            int64 // Angle at redistribution start

	// Fire flash effect
	FlashRemaining time.Duration
}