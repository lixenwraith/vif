package parameter

// Flocking separation
var (
	// FlockingSeparationRadiusX is horizontal separation zone (cells)
	FlockingSeparationRadiusX = 8.0

	// FlockingSeparationRadiusY is vertical separation zone (cells, at the reference aspect)
	FlockingSeparationRadiusY = 4.0
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
