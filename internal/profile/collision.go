package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// Offset blend; mirrors collision.go
const (
	OffsetNone float64 = 0
	OffsetBody float64 = 1.0 / 3.0
)

// kineticF builds a combat knockback profile
func kinetic(impactor, target Mass, mode physics.ImpulseMode, angleVar, offset float64) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       massRatio(impactor, target),
		ImpulseMin:      parameter.CollisionKineticImpulseMinFloat,
		ImpulseMax:      parameter.CollisionKineticImpulseMaxFloat,
		AngleVariance:   angleVar,
		Mode:            mode,
		OffsetInfluence: offset,
	}
}

// redirect sends the target off as a unit (cleaner, shield)
func redirect(impactor, target Mass, angleVar, offset float64) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseOverride, angleVar, offset)
}

// accumulate adds to existing velocity (explosion, dust)
func accumulate(impactor, target Mass, angleVar, offset float64) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseAdditive, angleVar, offset)
}

// soft builds an inter-species repulsion profile (scatter, not combat)
func soft(impactor, target Mass) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       softRatio(impactor, target),
		ImpulseMin:      parameter.SoftCollisionImpulseMinFloat,
		ImpulseMax:      parameter.SoftCollisionImpulseMaxFloat,
		AngleVariance:   parameter.SoftCollisionAngleVarFloat,
		Mode:            physics.ImpulseAdditive,
		OffsetInfluence: 0,
	}
}

// --- Cleaner (projectile) ---
var (
	CleanerToDrain     = accumulate(MassCleaner, MassDrain, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	CleanerToSwarm     = redirect(MassCleaner, MassSwarm, parameter.SwarmDeflectAngleVarFloat, OffsetBody)
	CleanerToQuasar    = redirect(MassCleaner, MassQuasar, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	CleanerToStorm     = redirect(MassCleaner, MassStorm, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	CleanerToSnakeHead = redirect(MassCleaner, MassSnakeHead, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	CleanerToSnakeBody = accumulate(MassCleaner, MassSnakeBody, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	CleanerToEye       = redirect(MassCleaner, MassEye, parameter.DrainDeflectAngleVarFloat, OffsetBody)
)

// --- Shield ---
var (
	ShieldToDrain     = accumulate(MassCursor, MassDrain, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	ShieldToSwarm     = redirect(MassCursor, MassSwarm, parameter.SwarmDeflectAngleVarFloat, OffsetBody)
	ShieldToQuasar    = redirect(MassCursor, MassQuasar, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ShieldToStorm     = redirect(MassCursor, MassStorm, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ShieldToSnakeHead = redirect(MassCursor, MassSnakeHead, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ShieldToSnakeBody = accumulate(MassCursor, MassSnakeBody, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	ShieldToEye       = redirect(MassCursor, MassEye, parameter.DrainDeflectAngleVarFloat, OffsetBody)
)

// --- Explosion (also used by missile impact) ---
var (
	ExplosionToDrain     = accumulate(MassExplosion, MassDrain, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	ExplosionToSwarm     = accumulate(MassExplosion, MassSwarm, parameter.SwarmDeflectAngleVarFloat, OffsetBody)
	ExplosionToQuasar    = accumulate(MassExplosion, MassQuasar, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ExplosionToStorm     = accumulate(MassExplosion, MassStorm, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ExplosionToSnakeHead = accumulate(MassExplosion, MassSnakeHead, parameter.DrainDeflectAngleVarFloat, OffsetBody)
	ExplosionToSnakeBody = accumulate(MassExplosion, MassSnakeBody, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	ExplosionToEye       = accumulate(MassExplosion, MassEye, parameter.DrainDeflectAngleVarFloat, OffsetBody)
)

// --- Dust ---
var (
	DustToDrain     = accumulate(MassDust, MassDrain, parameter.DrainDeflectAngleVarFloat, OffsetNone)
	DustToComposite = accumulate(MassDust, MassQuasar, parameter.DrainDeflectAngleVarFloat, OffsetNone)
)

// --- Soft collision (inter-species scatter) ---
var (
	SoftDrainToQuasar  = soft(MassDrain, MassQuasar)
	SoftSwarmToSwarm   = soft(MassSwarm, MassSwarm)
	SoftSwarmToQuasar  = soft(MassSwarm, MassQuasar)
	SoftQuasarToSwarm  = soft(MassQuasar, MassSwarm)
	SoftQuasarToDrain  = soft(MassQuasar, MassDrain)
	SoftQuasarToQuasar = soft(MassQuasar, MassQuasar)
	SoftStormToSwarm   = soft(MassStorm, MassSwarm)
	SoftStormToQuasar  = soft(MassStorm, MassQuasar)
	SoftPylonToDrain   = soft(MassPylon, MassDrain)
	SoftPylonToSwarm   = soft(MassPylon, MassSwarm)
	SoftPylonToQuasar  = soft(MassPylon, MassQuasar)
)
