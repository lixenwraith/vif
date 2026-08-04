package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Loot physics
var (
	LootChaseSpeed    = vmath.FromFloat(LootHomingMaxSpeedFloat)
	LootHomingAccel   = vmath.FromFloat(LootHomingAccelFloat)
	LootBurstSpeed    = vmath.FromFloat(LootBurstSpeedFloat)
	LootRestitution   = vmath.FromFloat(LootRestitutionFloat)
	LootFlowLookahead = vmath.FromFloat(LootFlowLookaheadFloat)
	LootVelocityBleed = vmath.FromFloat(LootVelocityBleedFloat)
	LootStopSpeed     = vmath.FromFloat(LootStopSpeedFloat)
)

// Heat loot reward value
const LootHeatRewardValue = 10

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

// Movement
const (
	// LootBurstSpeedFloat is initial scatter velocity on drop (cells/sec)
	LootBurstSpeedFloat = 8.0
	// LootRestitutionFloat is velocity retained on wall bounce
	LootRestitutionFloat = 0.4
	// LootFlowLookaheadFloat is flow-field target lookahead when LOS is blocked (cells)
	LootFlowLookaheadFloat = 5.0
	// LootVelocityBleedFloat is velocity decay when stuck with no flow (1/sec)
	LootVelocityBleedFloat = 6.0
	// LootStopSpeedFloat is the per-axis speed below which a stuck loot snaps to rest
	LootStopSpeedFloat = 0.1
)
