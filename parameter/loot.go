package parameter

import (
	"time"
)

// Energy loot reward value
const LootEnergyRewardValue = 10000

// Homing physics
const (
	LootHomingAccelFloat    = 120.0
	LootHomingMaxSpeedFloat = 60.0
)

// Collection radius (Chebyshev)
const LootCollectRadius = 2

// Shield geometry (shared across all loot types)
const (
	LootShieldRadiusX    = 2.5
	LootShieldRadiusY    = 1.5
	LootShieldMaxOpacity = 0.5
)

// Glow effect
const (
	LootGlowRotationPeriod = 500 * time.Millisecond
	LootGlowEdgeThreshold  = 0.25
	LootGlowIntensity      = 0.7
)