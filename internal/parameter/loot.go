package parameter

import (
	"time"
)

// Heat loot reward value
const LootHeatRewardValue = 10

// Energy loot reward value
const LootEnergyRewardValue = 10000

// Homing physics
const (
	LootHomingAccel    = 120.0
	LootHomingMaxSpeed = 60.0
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

// Movement
const (
	// LootBurstSpeed is initial scatter velocity on drop (cells/sec)
	LootBurstSpeed = 8.0
	// LootRestitution is velocity retained on wall bounce
	LootRestitution = 0.4
	// LootFlowLookahead is flow-field target lookahead when LOS is blocked (cells)
	LootFlowLookahead = 5.0
	// LootVelocityBleed is velocity decay when stuck with no flow (1/sec)
	LootVelocityBleed = 6.0
	// LootStopSpeed is the per-axis speed below which a stuck loot snaps to rest
	LootStopSpeed = 0.1
)
