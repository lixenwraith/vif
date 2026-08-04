package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// Offset blend; mirrors collision.go
const (
	OffsetNoneF float64 = 0
	OffsetBodyF float64 = 1.0 / 3.0
)

// kineticF builds a combat knockback profile
func kineticF(impactor, target MassF, mode physics.ImpulseMode, angleVar, offset float64) physics.CollisionProfileF {
	return physics.CollisionProfileF{
		MassRatio:       massRatioF(impactor, target),
		ImpulseMin:      parameter.CollisionKineticImpulseMinFloat,
		ImpulseMax:      parameter.CollisionKineticImpulseMaxFloat,
		AngleVariance:   angleVar,
		Mode:            mode,
		OffsetInfluence: offset,
	}
}

// redirectF sends the target off as a unit (cleaner, shield)
func redirectF(impactor, target MassF, angleVar, offset float64) physics.CollisionProfileF {
	return kineticF(impactor, target, physics.ImpulseOverride, angleVar, offset)
}

// accumulateF adds to existing velocity (explosion, dust)
func accumulateF(impactor, target MassF, angleVar, offset float64) physics.CollisionProfileF {
	return kineticF(impactor, target, physics.ImpulseAdditive, angleVar, offset)
}

// softF builds an inter-species repulsion profile (scatter, not combat)
func softF(impactor, target MassF) physics.CollisionProfileF {
	return physics.CollisionProfileF{
		MassRatio:       softRatioF(impactor, target),
		ImpulseMin:      parameter.SoftCollisionImpulseMinFloat,
		ImpulseMax:      parameter.SoftCollisionImpulseMaxFloat,
		AngleVariance:   parameter.SoftCollisionAngleVarFloat,
		Mode:            physics.ImpulseAdditive,
		OffsetInfluence: 0,
	}
}

// --- Cleaner (projectile) ---
var (
	CleanerToDrainF     = accumulateF(MassCleanerF, MassDrainF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	CleanerToSwarmF     = redirectF(MassCleanerF, MassSwarmF, parameter.SwarmDeflectAngleVarFloat, OffsetBodyF)
	CleanerToQuasarF    = redirectF(MassCleanerF, MassQuasarF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	CleanerToStormF     = redirectF(MassCleanerF, MassStormF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	CleanerToSnakeHeadF = redirectF(MassCleanerF, MassSnakeHeadF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	CleanerToSnakeBodyF = accumulateF(MassCleanerF, MassSnakeBodyF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	CleanerToEyeF       = redirectF(MassCleanerF, MassEyeF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
)

// --- Shield ---
var (
	ShieldToDrainF     = accumulateF(MassCursorF, MassDrainF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	ShieldToSwarmF     = redirectF(MassCursorF, MassSwarmF, parameter.SwarmDeflectAngleVarFloat, OffsetBodyF)
	ShieldToQuasarF    = redirectF(MassCursorF, MassQuasarF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ShieldToStormF     = redirectF(MassCursorF, MassStormF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ShieldToSnakeHeadF = redirectF(MassCursorF, MassSnakeHeadF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ShieldToSnakeBodyF = accumulateF(MassCursorF, MassSnakeBodyF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	ShieldToEyeF       = redirectF(MassCursorF, MassEyeF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
)

// --- Explosion (also used by missile impact) ---
var (
	ExplosionToDrainF     = accumulateF(MassExplosionF, MassDrainF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	ExplosionToSwarmF     = accumulateF(MassExplosionF, MassSwarmF, parameter.SwarmDeflectAngleVarFloat, OffsetBodyF)
	ExplosionToQuasarF    = accumulateF(MassExplosionF, MassQuasarF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ExplosionToStormF     = accumulateF(MassExplosionF, MassStormF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ExplosionToSnakeHeadF = accumulateF(MassExplosionF, MassSnakeHeadF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
	ExplosionToSnakeBodyF = accumulateF(MassExplosionF, MassSnakeBodyF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	ExplosionToEyeF       = accumulateF(MassExplosionF, MassEyeF, parameter.DrainDeflectAngleVarFloat, OffsetBodyF)
)

// --- Dust ---
var (
	DustToDrainF     = accumulateF(MassDustF, MassDrainF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
	DustToCompositeF = accumulateF(MassDustF, MassQuasarF, parameter.DrainDeflectAngleVarFloat, OffsetNoneF)
)

// --- Soft collision (inter-species scatter) ---
var (
	SoftDrainToQuasarF  = softF(MassDrainF, MassQuasarF)
	SoftSwarmToSwarmF   = softF(MassSwarmF, MassSwarmF)
	SoftSwarmToQuasarF  = softF(MassSwarmF, MassQuasarF)
	SoftQuasarToSwarmF  = softF(MassQuasarF, MassSwarmF)
	SoftQuasarToDrainF  = softF(MassQuasarF, MassDrainF)
	SoftQuasarToQuasarF = softF(MassQuasarF, MassQuasarF)
	SoftStormToSwarmF   = softF(MassStormF, MassSwarmF)
	SoftStormToQuasarF  = softF(MassStormF, MassQuasarF)
	SoftPylonToDrainF   = softF(MassPylonF, MassDrainF)
	SoftPylonToSwarmF   = softF(MassPylonF, MassSwarmF)
	SoftPylonToQuasarF  = softF(MassPylonF, MassQuasarF)
)
