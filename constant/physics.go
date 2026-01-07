package constant

import "github.com/lixenwraith/vi-fighter/vmath"

// Pre-computed Q32.32 physics constants
// Initialized once, used by systems to avoid repeated FromFloat calls
var (
	// Drain physics (Q32.32)
	DrainBaseSpeed         = vmath.FromFloat(DrainBaseSpeedFloat)
	DrainHomingAccel       = vmath.FromFloat(DrainHomingAccelFloat)
	DrainDrag              = vmath.FromFloat(DrainDragFloat)
	DrainDeflectImpulse    = vmath.FromFloat(DrainDeflectImpulseFloat)
	DrainDeflectAngleVar   = vmath.FromFloat(DrainDeflectAngleVarFloat)
	DrainDeflectImpulseMin = vmath.FromFloat(DrainDeflectImpulseMinFloat)
	DrainDeflectImpulseMax = vmath.FromFloat(DrainDeflectImpulseMaxFloat)

	// Quasar physics (Q32.32)
	QuasarDeflectImpulseMin = vmath.FromFloat(QuasarDeflectImpulseMinFloat)
	QuasarDeflectImpulseMax = vmath.FromFloat(QuasarDeflectImpulseMaxFloat)
	QuasarHomingAccel       = vmath.FromFloat(QuasarHomingAccelFloat)
	QuasarBaseSpeed         = vmath.FromFloat(QuasarBaseSpeedFloat)
	QuasarDrag              = vmath.FromFloat(QuasarDragFloat)
	// QuasarSpeedMultiplierMaxFixed caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMaxFixed = vmath.Scale * QuasarSpeedMultiplierMax

	// Shield knockback physics (Q32.32)
	ShieldKnockbackImpulseMin = vmath.FromFloat(ShieldKnockbackImpulseMinFloat)
	ShieldKnockbackImpulseMax = vmath.FromFloat(ShieldKnockbackImpulseMaxFloat)

	// Dust physics (Q32.32)
	DustAttractionBase = vmath.FromFloat(DustAttractionBaseFloat)
	DustOrbitRadiusMin = vmath.FromFloat(DustOrbitRadiusMinFloat)
	DustOrbitRadiusMax = vmath.FromFloat(DustOrbitRadiusMaxFloat)
	DustDamping        = vmath.FromFloat(DustDampingFloat)
	DustChaseBoost     = vmath.FromFloat(DustChaseBoostFloat)
	DustChaseDecay     = vmath.FromFloat(DustChaseDecayFloat)
	DustInitialSpeed   = vmath.FromFloat(DustInitialSpeedFloat)
	// Dynamic scaling constants
	DustBoostMax       = vmath.FromFloat(DustBoostMaxFloat)
	DustShieldRedirect = vmath.FromFloat(DustShieldRedirectFloat)
)

// Explosion field VFX (Q32.32)
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