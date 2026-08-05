package parameter

import (
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Storm circle dimensions (ellipse, terminal 2:1 aspect)
const (
	StormCircleRadiusX = 10.0
	StormCircleRadiusY = 5.0
)

// Storm combat
const (
	StormCircleCollisionRadius = 7.5

	// CombatInitialHPStormMember is baseline HP for each Storm circle member
	CombatInitialHPStormMember = 10
)

// Storm spawn geometry
const (
	StormInitialRadius = 20.0
	StormInitialSpeed  = 8.0
)

// Storm 3D physics (tuned for three-body chaos)
const (
	StormGravity           = 80.0
	StormRepulsionRadius   = 18.0
	StormRepulsionStrength = 250.0
	StormDamping           = 0.92
	StormMaxVelocity       = 45.0
	StormRestitution       = 1.0
	StormZMin              = 3.0
	StormZMax              = 30.0
	StormZMid              = (StormZMin + StormZMax) / 2
	StormZSpawnOffset      = 10.0
)

// Storm Z-axis stability (anti-deadlock)
const (
	// StormZEquilibriumStiffness is spring constant toward zMid (cells/sec²)
	// Higher = faster oscillation, lower = gentler correction
	StormZEquilibriumStiffness = 12.0

	// StormInvulnerabilityMaxDurationMs is max continuous invulnerability before nudge (ms)
	StormInvulnerabilityMaxDurationMs = 3000

	// StormInvulnerabilityNudge is downward velocity impulse on timeout (cells/sec)
	StormInvulnerabilityNudge = 8.0
)

// Storm boundary insets (account for visual radius)
const (
	StormBoundaryInsetX = 11.0
	StormBoundaryInsetY = 6.0
)

// Storm glow (near/convex/vulnerable state) and halo (far/concave/vulnerable state)
const (
	StormConcaveHaloExtend = 4.0 // Cell extension beyond body

	StormConvexGlowExtend       = 2.0 // Cell extension beyond body
	StormConvexGlowPeriodMs     = 942 // Pulse period in milliseconds (~150ms/radian * 2π)
	StormConvexGlowIntensityMin = 0.5 // Base intensity at pulse trough
	StormConvexGlowIntensityMax = 1.2 // Peak intensity (0.5 + 0.7)
	StormConvexGlowOuterDistSq  = 1.6 // Cutoff normalized distance squared
	StormConvexGlowFalloffMult  = 4.0 // Alpha falloff multiplier
)

// Pre-computed
var (
	StormCircleCollisionInvRxSq,
	StormCircleCollisionInvRySq = vmath.EllipseInvRadiiSqF(StormCircleRadiusX, StormCircleRadiusY)
	StormInvulnerabilityMaxDuration = time.Duration(StormInvulnerabilityMaxDurationMs) * time.Millisecond
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

	StormRedBulletSpeed           = 50.0 // cells/sec
	StormRedBulletSpreadHalfAngle = 0.32 // radians (~18°)
	StormRedBulletSpawnMargin     = 1.15 // multiplier outside ellipse boundary
	StormRedBulletMaxLifetime     = 4 * time.Second

	// Blue circle: swarm spawn
	StormBlueInitialCooldown    = 5 * time.Second
	StormBlueRepeatCooldown     = 5 * time.Second
	StormBlueEffectDuration     = 2500 * time.Millisecond // 2s glow + 0.5s overlap with materialize
	StormBlueMaterializeAt      = 0.80                    // Emit materialize at 80% progress (2s mark)
	StormBlueGlowRotationPeriod = 400 * time.Millisecond  // ms per full rotation (5 rotations in 2s)
	StormBlueSpawnDistance      = 10.0
)

// Pre-computed green attack radii (2× circle radius)
var (
	StormGreenRadiusX = StormCircleRadiusX * StormGreenRadiusMultiplier
	StormGreenRadiusY = StormCircleRadiusY * StormGreenRadiusMultiplier
	StormGreenInvRxSq,
	StormGreenInvRySq = vmath.EllipseInvRadiiSqF(StormGreenRadiusX, StormGreenRadiusY)
)
