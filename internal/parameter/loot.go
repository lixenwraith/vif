package parameter

import (
	"time"
)

// Heat loot reward value
const LootHeatRewardValue = 10

// Energy loot reward value
const LootEnergyRewardValue = 10000

// Homing physics.
//
// The two are read together. With the cornering brake every homing entity applies,
// a drop settles at roughly LootHomingAccel / (2 * NavCorneringBrake) cells per
// second on a straight approach — about 19 here, a cell per tick, which is brisk
// without crossing a corridor between two collision samples.
//
// The pair also decides whether a drop can circle its owner instead of reaching it.
// A constant attraction with nothing damping the sideways component holds a
// circular orbit at radius speed² / accel: at the previous 60 and 120 that radius
// was thirty cells, far outside LootHomingArrivalRadius where the profile's arrival
// damping lives, so a drop knocked sideways orbited the cursor until something else
// moved it. Keep the ratio inside the arrival radius.
const (
	LootHomingAccel    = 60.0
	LootHomingMaxSpeed = 34.0
)

// Homing profile shape (see profile.LootHoming)
const (
	// LootHomingDrag is the deceleration applied above the cruising speed. It is
	// the ceiling the cornering brake does not set, and it was too weak to be one:
	// at 2.0 a drop accelerating from a receding flow target passed the cruising
	// speed and kept going.
	LootHomingDrag = 6.0
	// LootHomingArrivalRadius is where approach damping begins (cells)
	LootHomingArrivalRadius = 5.0
	// LootHomingArrivalDrag is the drag multiplier at the target
	LootHomingArrivalDrag = 25.0
	// LootHomingDeadZone is the snap-to-target radius (cells)
	LootHomingDeadZone = 0.5
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
	// LootFlowLookahead caps the flow-field target lookahead when the way to the
	// owner is blocked (cells). It is a cap rather than a fixed distance: a target
	// held a constant distance ahead recedes exactly as fast as the drop closes on
	// it, so the profile's arrival damping never engages and the drop takes a
	// corner at full cruising speed.
	LootFlowLookahead = 4.0
	// LootVelocityBleed is velocity decay when stuck with no flow (1/sec)
	LootVelocityBleed = 6.0
	// LootStopSpeed is the per-axis speed below which a stuck loot snaps to rest
	LootStopSpeed = 0.1
)
