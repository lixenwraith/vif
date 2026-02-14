package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Storm spawn trigger
const SwarmKillsForStorm = 10

// Storm circle dimensions (ellipse, terminal 2:1 aspect)
const (
	StormCircleRadiusXFloat = 10.0
	StormCircleRadiusYFloat = 5.0
)

// Storm combat
const (
	StormCircleRadiusCollisionFloat = 7.5

	// CombatInitialHPStormMember is baseline HP for each Storm circle member
	CombatInitialHPStormMember = 10
)

// Storm spawn geometry
const (
	StormInitialRadiusFloat = 20.0
	StormInitialSpeedFloat  = 8.0
)

// Storm 3D physics (tuned for three-body chaos)
const (
	StormGravityFloat           = 80.0
	StormRepulsionRadiusFloat   = 18.0
	StormRepulsionStrengthFloat = 250.0
	StormDampingFloat           = 0.92
	StormMaxVelocityFloat       = 45.0
	StormRestitutionFloat       = 1.0
	StormZMinFloat              = 3.0
	StormZMaxFloat              = 30.0
	StormZMidFloat              = (StormZMinFloat + StormZMaxFloat) / 2
	StormZSpawnOffsetFloat      = 10.0
)

// Storm Z-axis stability (anti-deadlock)
const (
	// StormZEquilibriumStiffnessFloat is spring constant toward zMid (cells/sec²)
	// Higher = faster oscillation, lower = gentler correction
	StormZEquilibriumStiffnessFloat = 12.0

	// StormInvulnerabilityMaxDurationMs is max continuous invulnerability before nudge (ms)
	StormInvulnerabilityMaxDurationMs = 3000

	// StormInvulnerabilityNudgeFloat is downward velocity impulse on timeout (cells/sec)
	StormInvulnerabilityNudgeFloat = 8.0
)

// Storm boundary insets (account for visual radius)
const (
	StormBoundaryInsetXFloat = 11.0
	StormBoundaryInsetYFloat = 6.0
)

// Storm glow (near/convex/vulnerable state) and halo (far/concave/vulnerable state)
const (
	StormConcaveHaloExtendFloat = 4.0 // Cell extension beyond body

	StormConvexGlowExtendFloat      = 2.0 // Cell extension beyond body
	StormConvexGlowPeriodMs         = 942 // Pulse period in milliseconds (~150ms/radian * 2π)
	StormConvexGlowIntensityMin     = 0.5 // Base intensity at pulse trough
	StormConvexGlowIntensityMax     = 1.2 // Peak intensity (0.5 + 0.7)
	StormConvexGlowOuterDistSqFloat = 1.6 // Cutoff normalized distance squared
	StormConvexGlowFalloffMult      = 4.0 // Alpha falloff multiplier
)

// Pre-computed Q32.32 values
var (
	StormCircleRadiusX = vmath.FromFloat(StormCircleRadiusXFloat)
	StormCircleRadiusY = vmath.FromFloat(StormCircleRadiusYFloat)

	StormGravity           = vmath.FromFloat(StormGravityFloat)
	StormRepulsionRadius   = vmath.FromFloat(StormRepulsionRadiusFloat)
	StormRepulsionStrength = vmath.FromFloat(StormRepulsionStrengthFloat)
	StormDamping           = vmath.FromFloat(StormDampingFloat)
	StormMaxVelocity       = vmath.FromFloat(StormMaxVelocityFloat)
	StormRestitution       = vmath.FromFloat(StormRestitutionFloat)
	StormZMin              = vmath.FromFloat(StormZMinFloat)
	StormZMax              = vmath.FromFloat(StormZMaxFloat)
	StormZSpawnOffset      = vmath.FromFloat(StormZSpawnOffsetFloat)

	StormBoundaryInsetX = int(StormBoundaryInsetXFloat)
	StormBoundaryInsetY = int(StormBoundaryInsetYFloat)

	StormCollisionRadius = vmath.FromFloat(StormCircleRadiusCollisionFloat)
	StormCollisionInvRxSq,
	StormCollisionInvRySq = vmath.EllipseInvRadiiSq(StormCircleRadiusX, StormCircleRadiusY)

	StormZEquilibriumStiffness      = vmath.FromFloat(StormZEquilibriumStiffnessFloat)
	StormInvulnerabilityMaxDuration = time.Duration(StormInvulnerabilityMaxDurationMs) * time.Millisecond
	StormInvulnerabilityNudge       = vmath.FromFloat(StormInvulnerabilityNudgeFloat)

	// Precompute zMid for physics
	StormZMid = (StormZMin + StormZMax) / 2
)

// Storm circle attack parameters
const (
	// Green circle: area pulse attack
	StormGreenInitialCooldown  = 1 * time.Second
	StormGreenRepeatInterval   = 1 * time.Second
	StormGreenRadiusMultiplier = 3.0 // Multiplier to circle radius in each dimension
	StormGreenDamageEnergy     = 10000
	StormGreenDamageHeat       = 10

	// Red circle: cone projectile attack
	StormRedInitialCooldown    = 1 * time.Second
	StormRedTravelDuration     = 2 * time.Second
	StormRedPostAttackDelay    = 1 * time.Second // Wait after travel before next shot
	StormRedConeWidthCells     = 40
	StormRedConeHeightCells    = 60
	StormRedDamageEnergy       = 10000
	StormRedDamageHeat         = 10
	StormRedDamageBulletEnergy = 100

	StormRedBulletSpeedFloat      = 50.0 // cells/sec
	StormRedBulletSpreadHalfAngle = 0.32 // radians (~18°)
	StormRedBulletSpawnMargin     = 1.15 // multiplier outside ellipse boundary
	StormRedBulletMaxLifetime     = 4 * time.Second

	// Blue circle: swarm spawn
	StormBlueInitialCooldown    = 5 * time.Second
	StormBlueRepeatCooldown     = 5 * time.Second
	StormBlueEffectDuration     = 2500 * time.Millisecond // 2s glow + 0.5s overlap with materialize
	StormBlueMaterializeAt      = 0.80                    // Emit materialize at 80% progress (2s mark)
	StormBlueGlowRotationPeriod = 400 * time.Millisecond  // ms per full rotation (5 rotations in 2s)
	StormBlueSpawnDistanceFloat = 20.0
)

// Pre-computed green attack radii (2× circle radius)
var (
	StormGreenRadiusX = vmath.FromFloat(StormCircleRadiusXFloat * StormGreenRadiusMultiplier)
	StormGreenRadiusY = vmath.FromFloat(StormCircleRadiusYFloat * StormGreenRadiusMultiplier)
	StormGreenInvRxSq,
	StormGreenInvRySq = vmath.EllipseInvRadiiSq(StormGreenRadiusX, StormGreenRadiusY)
)