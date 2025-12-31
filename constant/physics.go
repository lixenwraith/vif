package constant

// @lixen: #dev{feature[drain(render,system)]}

import "github.com/lixenwraith/vi-fighter/vmath"

// Pre-computed Q16.16 physics constants
// Initialized once, used by systems to avoid repeated FromFloat calls
var (
	DrainBaseSpeed         = vmath.FromFloat(DrainBaseSpeedFloat)
	DrainHomingAccel       = vmath.FromFloat(DrainHomingAccelFloat)
	DrainDrag              = vmath.FromFloat(DrainDragFloat)
	DrainDeflectImpulse    = vmath.FromFloat(DrainDeflectImpulseFloat)
	DrainDeflectAngleVar   = vmath.FromFloat(DrainDeflectAngleVarFloat)
	DrainDeflectImpulseMin = vmath.FromFloat(DrainDeflectImpulseMinFloat)
	DrainDeflectImpulseMax = vmath.FromFloat(DrainDeflectImpulseMaxFloat)
)