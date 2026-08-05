package parameter

import "github.com/lixenwraith/vi-fighter/pkg/vmath"

// Flocking separation
var (
	// FlockingSeparationRadiusX is horizontal separation zone (cells)
	FlockingSeparationRadiusX = 8.0

	// FlockingSeparationRadiusY is vertical separation zone (cells, aspect-corrected)
	FlockingSeparationRadiusY = 4.0

	// Pre-computed inverse squared radii for ellipse overlap checks
	FlockingSeparationInvRxSq, FlockingSeparationInvRySq = vmath.EllipseInvRadiiSqF(FlockingSeparationRadiusX, FlockingSeparationRadiusY)
)

// Explosion field VFX
var (
	ExplosionMergeThresholdSq = ExplosionMergeThreshold * ExplosionMergeThreshold
	ExplosionRadiusCapFixed   = ExplosionFieldRadius * ExplosionRadiusCapMultiplier

	// Factor = 1.0 / Midpoint (2.0 for 0.5)
	ExplosionGradientFactor = 1.0 / ExplosionGradientMidpoint
)

// Missile physics
var (
	MissileImpactRadiusSq = MissileImpactRadius * MissileImpactRadius
)

// Pulse physics
var (
	PulseRadiusInvRxSq, PulseRadiusInvRySq = vmath.EllipseInvRadiiSqF(PulseRadiusX, PulseRadiusY)
)

// Pylon collision
var (
	PylonCollisionInvRxSq, PylonCollisionInvRySq = vmath.EllipseInvRadiiSqF(PylonCollisionRadiusX, PylonCollisionRadiusY)
)
