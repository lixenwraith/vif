package parameter

import "github.com/lixenwraith/vi-fighter/vmath"

// Pre-computed Q32.32 physics constants, initialized once to avoid repeated float calculation and used by systems

// Collision kinetic impulse
var (
	CollisionKineticImpulseMin = vmath.FromFloat(CollisionKineticImpulseMinFloat)
	CollisionKineticImpulseMax = vmath.FromFloat(CollisionKineticImpulseMaxFloat)
)

// Cleaner physics
var (
	CleanerBaseHorizontalSpeed = vmath.FromFloat(CleanerBaseHorizontalSpeedFloat)
	CleanerBaseVerticalSpeed   = vmath.FromFloat(CleanerBaseVerticalSpeedFloat)
	CleanerTrailLenFixed       = vmath.FromInt(CleanerTrailLength)
)

// Drain physics
var (
	// Drain physics
	DrainBaseSpeed       = vmath.FromFloat(DrainBaseSpeedFloat)
	DrainHomingAccel     = vmath.FromFloat(DrainHomingAccelFloat)
	DrainDrag            = vmath.FromFloat(DrainDragFloat)
	DrainDeflectAngleVar = vmath.FromFloat(DrainDeflectAngleVarFloat)
)

// Quasar physics
var (
	// Quasar physics
	QuasarHomingAccel = vmath.FromFloat(QuasarHomingAccelFloat)
	QuasarBaseSpeed   = vmath.FromFloat(QuasarBaseSpeedFloat)
	QuasarMaxSpeed    = vmath.FromFloat(QuasarMaxSpeedFloat)
	QuasarDrag        = vmath.FromFloat(QuasarDragFloat)
	// QuasarSpeedMultiplierMaxFixed caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMaxFixed = vmath.Scale * QuasarSpeedMultiplierMax
)

// Swarm physics
var (
	SwarmChaseSpeed      = vmath.Mul(DrainBaseSpeed, vmath.FromInt(SwarmChaseSpeedMultiplier))
	SwarmHomingAccel     = vmath.FromFloat(SwarmHomingAccelFloat)
	SwarmDrag            = vmath.FromFloat(SwarmDragFloat)
	SwarmDeflectAngleVar = vmath.FromFloat(SwarmDeflectAngleVarFloat)
)

// Soft collision impulse (inter-enemy repulsion)
var (
	SoftCollisionImpulseMin = vmath.FromFloat(SoftCollisionImpulseMinFloat)
	SoftCollisionImpulseMax = vmath.FromFloat(SoftCollisionImpulseMaxFloat)
	SoftCollisionAngleVar   = vmath.FromFloat(SoftCollisionAngleVarFloat)
)

// Swarm flocking separation
var (
	SwarmSeparationRadiusX  = vmath.FromFloat(SwarmSeparationRadiusXFloat)
	SwarmSeparationRadiusY  = vmath.FromFloat(SwarmSeparationRadiusYFloat)
	SwarmSeparationStrength = vmath.FromFloat(SwarmSeparationStrengthFloat)
)

// Entity collision radii (ellipse semi-axes for overlap detection)
var (
	QuasarCollisionRadiusX = vmath.FromFloat(QuasarCollisionRadiusXFloat)
	QuasarCollisionRadiusY = vmath.FromFloat(QuasarCollisionRadiusYFloat)
	SwarmCollisionRadiusX  = vmath.FromFloat(SwarmCollisionRadiusXFloat)
	SwarmCollisionRadiusY  = vmath.FromFloat(SwarmCollisionRadiusYFloat)
	DrainCollisionRadius   = vmath.FromFloat(DrainCollisionRadiusFloat)
)

// Pre-computed inverse squared radii for ellipse overlap checks
var (
	QuasarCollisionInvRxSq, QuasarCollisionInvRySq = vmath.EllipseInvRadiiSq(QuasarCollisionRadiusX, QuasarCollisionRadiusY)
	SwarmCollisionInvRxSq, SwarmCollisionInvRySq   = vmath.EllipseInvRadiiSq(SwarmCollisionRadiusX, SwarmCollisionRadiusY)
	SwarmSeparationInvRxSq, SwarmSeparationInvRySq = vmath.EllipseInvRadiiSq(SwarmSeparationRadiusX, SwarmSeparationRadiusY)
)

// Dust physics
var (
	DustAttractionBase = vmath.FromFloat(DustAttractionBaseFloat)
	DustOrbitRadiusMin = vmath.FromFloat(DustOrbitRadiusMinFloat)
	DustOrbitRadiusMax = vmath.FromFloat(DustOrbitRadiusMaxFloat)
	DustDamping        = vmath.FromFloat(DustDampingFloat)
	DustChaseBoost     = vmath.FromFloat(DustChaseBoostFloat)
	DustChaseDecay     = vmath.FromFloat(DustChaseDecayFloat)
	DustInitialSpeed   = vmath.FromFloat(DustInitialSpeedFloat)
	DustGlobalDrag     = vmath.FromFloat(DustGlobalDragFloat)
	DustJitter         = vmath.FromFloat(DustJitterFloat)
)

// Explosion field VFX
var (
	ExplosionFieldRadius      = vmath.FromFloat(ExplosionFieldRadiusFloat)
	ExplosionMergeThreshold   = vmath.FromFloat(ExplosionMergeThresholdFloat)
	ExplosionMergeThresholdSq = vmath.Mul(ExplosionMergeThreshold, ExplosionMergeThreshold)
	ExplosionIntensityBoost   = vmath.FromFloat(ExplosionIntensityBoostFloat)
	ExplosionRadiusBoost      = vmath.FromFloat(ExplosionRadiusBoostFloat)
	ExplosionIntensityCap     = vmath.FromFloat(ExplosionIntensityCapFloat)
	ExplosionRadiusCapFixed   = vmath.Mul(ExplosionFieldRadius, vmath.FromFloat(ExplosionRadiusCapMultiplier))
	ExplosionCoreThreshold    = vmath.FromFloat(ExplosionCoreThresholdFloat)
	ExplosionBodyThreshold    = vmath.FromFloat(ExplosionBodyThresholdFloat)
	ExplosionEdgeThreshold    = vmath.FromFloat(ExplosionEdgeThresholdFloat)

	// Visual fixed-point constants
	ExplosionAlphaMax         = vmath.FromFloat(ExplosionAlphaMaxFloat)
	ExplosionAlphaMin         = vmath.FromFloat(ExplosionAlphaMinFloat)
	ExplosionGradientMidpoint = vmath.FromFloat(ExplosionGradientMidpointFloat)
	// Factor = 1.0 / Midpoint (2.0 for 0.5)
	ExplosionGradientFactor = vmath.FromFloat(1.0 / ExplosionGradientMidpointFloat)
)

// Orb physics
var (
	OrbOrbitRadiusX = vmath.FromFloat(OrbOrbitRadiusXFloat)
	OrbOrbitRadiusY = vmath.FromFloat(OrbOrbitRadiusYFloat)
	OrbOrbitSpeed   = vmath.FromFloat(OrbOrbitSpeedFloat)
)

// Missile physics
var (
	MissileClusterLaunchSpeed  = vmath.FromFloat(MissileClusterLaunchSpeedFloat)
	MissileClusterMinDistance  = vmath.FromFloat(MissileClusterMinDistanceFloat)
	MissileSeekerMaxSpeed      = vmath.FromFloat(MissileSeekerMaxSpeedFloat)
	MissileSeekerHomingAccel   = vmath.FromFloat(MissileSeekerHomingAccelFloat)
	MissileSeekerDrag          = vmath.FromFloat(MissileSeekerDragFloat)
	MissileSeekerSpreadAngle   = vmath.FromFloat(MissileSeekerSpreadAngleFloat)
	MissileSeekerArrivalRadius = vmath.FromFloat(MissileSeekerArrivalRadiusFloat)
	MissileImpactRadius        = vmath.FromFloat(MissileImpactRadiusFloat)
	MissileImpactRadiusSq      = vmath.Mul(MissileImpactRadius, MissileImpactRadius)
	MissileExplosionRadius     = vmath.FromFloat(MissileExplosionRadiusFloat)
	MissileSplitTravelFraction = vmath.FromFloat(MissileSplitTravelFractionFloat)
)

// Loot physics
var (
	LootChaseSpeed  = vmath.FromFloat(LootHomingMaxSpeedFloat)
	LootHomingAccel = vmath.FromFloat(LootHomingAccelFloat)
)