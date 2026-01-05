package physics

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Collision profiles - pre-defined for zero allocation in hot path

// CleanerToDrain defines cleaner-to-drain collision (equal mass, additive)
var CleanerToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       constant.DrainDeflectImpulseMin,
	ImpulseMax:       constant.DrainDeflectImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: constant.DrainDeflectImmunity,
	OffsetInfluence:  0,
}

// CleanerToQuasar defines cleaner-to-quasar collision (mass ratio, override for stun)
var CleanerToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioCleanerToQuasar,
	ImpulseMin:       constant.QuasarDeflectImpulseMin,
	ImpulseMax:       constant.QuasarDeflectImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: constant.QuasarHitFlashDuration,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// ShieldToDrain defines shield-to-drain knockback (radial repulsion)
var ShieldToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       constant.ShieldKnockbackImpulseMin,
	ImpulseMax:       constant.ShieldKnockbackImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: constant.ShieldKnockbackImmunity,
	OffsetInfluence:  0,
}

// ShieldToQuasar defines shield-to-quasar knockback (centroid-based)
var ShieldToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioCleanerToQuasar,
	ImpulseMin:       constant.ShieldKnockbackImpulseMin,
	ImpulseMax:       constant.ShieldKnockbackImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: constant.ShieldKnockbackImmunity,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// DustToDrain defines dust-to-drain collision (light impactor, cumulative)
var DustToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioDustToDrain,
	ImpulseMin:       constant.DrainDeflectImpulseMin,
	ImpulseMax:       constant.DrainDeflectImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  0,
}

// DustToQuasar defines dust-to-quasar collision (very light impactor, cumulative)
var DustToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioDustToQuasar,
	ImpulseMin:       constant.QuasarDeflectImpulseMin,
	ImpulseMax:       constant.QuasarDeflectImpulseMax,
	AngleVariance:    constant.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// Homing profiles

// DrainHoming defines drain entity homing behavior
var DrainHoming = HomingProfile{
	BaseSpeed:        constant.DrainBaseSpeed,
	HomingAccel:      constant.DrainHomingAccel,
	Drag:             constant.DrainDrag,
	ArrivalRadius:    0, // No arrival steering
	ArrivalDragBoost: 0,
	DeadZone:         0, // Use default settling
}

// QuasarHoming defines quasar entity homing behavior with arrival steering
var QuasarHoming = HomingProfile{
	BaseSpeed:        constant.QuasarBaseSpeed,
	HomingAccel:      constant.QuasarHomingAccel,
	Drag:             constant.QuasarDrag,
	ArrivalRadius:    vmath.FromFloat(3.0), // Begin arrival steering at 3 cells
	ArrivalDragBoost: vmath.FromFloat(3.0), // 4x drag at target (1 + 3)
	DeadZone:         vmath.Scale / 2,      // Snap at 0.5 cells
}