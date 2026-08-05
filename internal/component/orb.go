package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// OrbComponent tracks orbital state for weapon visualization orbs
type OrbComponent struct {
	WeaponType  WeaponType
	OwnerEntity core.Entity

	// Current angle on orbit radian
	OrbitAngle float64

	// Assigned target angle from distribution radian
	TargetAngle float64

	// Orbit parameters
	OrbitRadiusX float64
	OrbitRadiusY float64
	OrbitSpeed   float64 // Radians per second when orbiting freely

	// Animation state
	RedistributeRemaining time.Duration
	StartAngle            float64 // Angle at redistribution start

	// Fire flash effect
	FlashRemaining time.Duration
}
