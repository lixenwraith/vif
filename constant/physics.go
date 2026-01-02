package constant

// @lixen: #dev{base(core),feature[drain(render,system)],feature[dust(render,system)],feature[quasar(render,system)]}

import "github.com/lixenwraith/vi-fighter/vmath"

// Pre-computed Q16.16 physics constants
// Initialized once, used by systems to avoid repeated FromFloat calls
var (
	// Drain physics (Q16.16)
	DrainBaseSpeed         = vmath.FromFloat(DrainBaseSpeedFloat)
	DrainHomingAccel       = vmath.FromFloat(DrainHomingAccelFloat)
	DrainDrag              = vmath.FromFloat(DrainDragFloat)
	DrainDeflectImpulse    = vmath.FromFloat(DrainDeflectImpulseFloat)
	DrainDeflectAngleVar   = vmath.FromFloat(DrainDeflectAngleVarFloat)
	DrainDeflectImpulseMin = vmath.FromFloat(DrainDeflectImpulseMinFloat)
	DrainDeflectImpulseMax = vmath.FromFloat(DrainDeflectImpulseMaxFloat)

	// Quasar physics (Q16.16)
	QuasarDeflectImpulseMin = vmath.FromFloat(QuasarDeflectImpulseMinFloat)
	QuasarDeflectImpulseMax = vmath.FromFloat(QuasarDeflectImpulseMaxFloat)
	QuasarHomingAccel       = vmath.FromFloat(QuasarHomingAccelFloat)
	QuasarBaseSpeed         = vmath.FromFloat(QuasarBaseSpeedFloat)
	QuasarDrag              = vmath.FromFloat(QuasarDragFloat)
	// QuasarSpeedMultiplierMaxFixed caps progressive speed increase (10x = Scale * 10)
	QuasarSpeedMultiplierMaxFixed = vmath.Scale * QuasarSpeedMultiplierMax

	// Shield knockback physics (Q16.16)
	ShieldKnockbackImpulseMin = vmath.FromFloat(ShieldKnockbackImpulseMinFloat)
	ShieldKnockbackImpulseMax = vmath.FromFloat(ShieldKnockbackImpulseMaxFloat)

	// Dust physics (Q16.16)
	DustAttractionBase = vmath.FromFloat(DustAttractionBaseFloat)
	DustOrbitRadiusMin = vmath.FromFloat(DustOrbitRadiusMinFloat)
	DustOrbitRadiusMax = vmath.FromFloat(DustOrbitRadiusMaxFloat)
	DustDamping        = vmath.FromFloat(DustDampingFloat)
	DustChaseBoost     = vmath.FromFloat(DustChaseBoostFloat)
	DustChaseDecay     = vmath.FromFloat(DustChaseDecayFloat)
	DustInitialSpeed   = vmath.FromFloat(DustInitialSpeedFloat)
)