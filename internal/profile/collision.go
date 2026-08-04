package profile

import (
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// Offset blends the hit point into the impulse direction. It applies only
// when the target's Kinetic lives on a header separate from the hit cell;
// single-cell entities and ablative members with their own Kinetic get none.
const (
	OffsetNone int64 = 0
	OffsetBody int64 = vmath.Scale / 3 // hit point contributes 1/3 of direction
)

// kinetic builds a combat knockback profile.
// mode is the attack family's accumulation rule; offset is the target's shape.
func kinetic(impactor, target Mass, mode physics.ImpulseMode, angleVar, offset int64) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       massRatio(impactor, target),
		ImpulseMin:      parameter.CollisionKineticImpulseMin,
		ImpulseMax:      parameter.CollisionKineticImpulseMax,
		AngleVariance:   angleVar,
		Mode:            mode,
		OffsetInfluence: offset,
	}
}

// redirect sends the target off as a unit (cleaner, shield)
func redirect(impactor, target Mass, angleVar, offset int64) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseOverride, angleVar, offset)
}

// accumulate adds to existing velocity (explosion, dust)
func accumulate(impactor, target Mass, angleVar, offset int64) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseAdditive, angleVar, offset)
}

// soft builds an inter-species repulsion profile (scatter, not combat)
func soft(impactor, target Mass) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       softRatio(impactor, target),
		ImpulseMin:      parameter.SoftCollisionImpulseMin,
		ImpulseMax:      parameter.SoftCollisionImpulseMax,
		AngleVariance:   parameter.SoftCollisionAngleVar,
		Mode:            physics.ImpulseAdditive,
		OffsetInfluence: 0,
	}
}

// --- Cleaner (projectile) ---
var (
	CleanerToDrain     = accumulate(MassCleaner, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
	CleanerToSwarm     = redirect(MassCleaner, MassSwarm, parameter.SwarmDeflectAngleVar, OffsetBody)
	CleanerToQuasar    = redirect(MassCleaner, MassQuasar, parameter.DrainDeflectAngleVar, OffsetBody)
	CleanerToStorm     = redirect(MassCleaner, MassStorm, parameter.DrainDeflectAngleVar, OffsetBody)
	CleanerToSnakeHead = redirect(MassCleaner, MassSnakeHead, parameter.DrainDeflectAngleVar, OffsetBody)
	CleanerToSnakeBody = accumulate(MassCleaner, MassSnakeBody, parameter.DrainDeflectAngleVar, OffsetNone)
	CleanerToEye       = redirect(MassCleaner, MassEye, parameter.DrainDeflectAngleVar, OffsetBody)
)

// --- Shield ---
// The shield is a cursor extension with no mass of its own; knockback is
// computed from cursor mass against the target.
var (
	ShieldToDrain     = accumulate(MassCursor, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
	ShieldToSwarm     = redirect(MassCursor, MassSwarm, parameter.SwarmDeflectAngleVar, OffsetBody)
	ShieldToQuasar    = redirect(MassCursor, MassQuasar, parameter.DrainDeflectAngleVar, OffsetBody)
	ShieldToStorm     = redirect(MassCursor, MassStorm, parameter.DrainDeflectAngleVar, OffsetBody)
	ShieldToSnakeHead = redirect(MassCursor, MassSnakeHead, parameter.DrainDeflectAngleVar, OffsetBody)
	ShieldToSnakeBody = accumulate(MassCursor, MassSnakeBody, parameter.DrainDeflectAngleVar, OffsetNone)
	ShieldToEye       = redirect(MassCursor, MassEye, parameter.DrainDeflectAngleVar, OffsetBody)
)

// --- Explosion (also used by missile impact) ---
// Additive throughout: explosions accumulate rather than redirect.
// Kinetic immunity gates the knockback; damage dedup uses RemainingDamageImmunity.
var (
	ExplosionToDrain     = accumulate(MassExplosion, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
	ExplosionToSwarm     = accumulate(MassExplosion, MassSwarm, parameter.SwarmDeflectAngleVar, OffsetBody)
	ExplosionToQuasar    = accumulate(MassExplosion, MassQuasar, parameter.DrainDeflectAngleVar, OffsetBody)
	ExplosionToStorm     = accumulate(MassExplosion, MassStorm, parameter.DrainDeflectAngleVar, OffsetBody)
	ExplosionToSnakeHead = accumulate(MassExplosion, MassSnakeHead, parameter.DrainDeflectAngleVar, OffsetBody)
	ExplosionToSnakeBody = accumulate(MassExplosion, MassSnakeBody, parameter.DrainDeflectAngleVar, OffsetNone)
	ExplosionToEye       = accumulate(MassExplosion, MassEye, parameter.DrainDeflectAngleVar, OffsetBody)
)

// --- Dust ---
// Dust impulses are accumulated per tick by DustSystem and applied in bulk;
// ApplyCollisionImpulse ignores OffsetInfluence, so shape is irrelevant here.
var (
	DustToDrain     = accumulate(MassDust, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
	DustToComposite = accumulate(MassDust, MassQuasar, parameter.DrainDeflectAngleVar, OffsetNone)
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
