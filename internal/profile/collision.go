package profile

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/pkg/physics"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// OffsetInfluenceDefault blends hit-point offset into impulse direction
// Scale/3 ≈ 0.33: offset contributes one third of the final direction
const OffsetInfluenceDefault = vmath.Scale / 3

// kinetic builds a combat knockback profile.
// Override redirects a composite as a unit; Additive accumulates on members.
// Offset is nonzero only when the hit point should angle the impulse.
func kinetic(impactor, target Mass, mode physics.ImpulseMode, angleVar, offset int64, immunity time.Duration) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:        massRatio(impactor, target),
		ImpulseMin:       parameter.CollisionKineticImpulseMin,
		ImpulseMax:       parameter.CollisionKineticImpulseMax,
		AngleVariance:    angleVar,
		Mode:             mode,
		ImmunityDuration: immunity,
		OffsetInfluence:  offset,
	}
}

// soft builds an inter-species repulsion profile (scatter, not combat)
func soft(impactor, target Mass) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:        massRatio(impactor, target),
		ImpulseMin:       parameter.SoftCollisionImpulseMin,
		ImpulseMax:       parameter.SoftCollisionImpulseMax,
		AngleVariance:    parameter.SoftCollisionAngleVar,
		Mode:             physics.ImpulseAdditive,
		ImmunityDuration: parameter.SoftCollisionImmunityDuration,
		OffsetInfluence:  0,
	}
}

// unit and member select impulse mode by target composition:
// a unit composite redirects wholesale, an ablative member takes a local push.
func unit(impactor, target Mass, angleVar int64, immunity time.Duration) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseOverride, angleVar, OffsetInfluenceDefault, immunity)
}

func member(impactor, target Mass, angleVar int64, immunity time.Duration) physics.CollisionProfile {
	return kinetic(impactor, target, physics.ImpulseAdditive, angleVar, 0, immunity)
}

// --- Cleaner (projectile) ---

var (
	CleanerToDrain     = member(MassCleaner, MassDrain, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToSwarm     = unit(MassCleaner, MassSwarm, parameter.SwarmDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToQuasar    = unit(MassCleaner, MassQuasar, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToStorm     = unit(MassCleaner, MassStorm, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToSnakeHead = unit(MassCleaner, MassSnakeHead, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToSnakeBody = member(MassCleaner, MassSnakeBody, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	CleanerToEye       = unit(MassCleaner, MassEye, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
)

// --- Shield ---
// The shield is a cursor extension with no mass of its own; knockback is
// computed from cursor mass against the target.

var (
	ShieldToDrain     = member(MassCursor, MassDrain, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToSwarm     = unit(MassCursor, MassSwarm, parameter.SwarmDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToQuasar    = unit(MassCursor, MassQuasar, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToStorm     = unit(MassCursor, MassStorm, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToSnakeHead = unit(MassCursor, MassSnakeHead, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToSnakeBody = member(MassCursor, MassSnakeBody, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ShieldToEye       = unit(MassCursor, MassEye, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
)

// --- Explosion (also used by missile impact) ---
// Additive throughout: explosions accumulate rather than redirect.
// Immunity is the hit-flash window, used for per-blast deduplication.

var (
	ExplosionToDrain     = member(MassExplosion, MassDrain, parameter.DrainDeflectAngleVar, parameter.CombatKineticImmunityDuration)
	ExplosionToSwarm     = member(MassExplosion, MassSwarm, parameter.SwarmDeflectAngleVar, parameter.CombatHitFlashDuration)
	ExplosionToQuasar    = member(MassExplosion, MassQuasar, parameter.DrainDeflectAngleVar, parameter.CombatHitFlashDuration)
	ExplosionToStorm     = member(MassExplosion, MassStorm, parameter.DrainDeflectAngleVar, parameter.CombatHitFlashDuration)
	ExplosionToSnakeHead = kinetic(MassExplosion, MassSnakeHead, physics.ImpulseAdditive, parameter.DrainDeflectAngleVar, OffsetInfluenceDefault, parameter.CombatHitFlashDuration)
	ExplosionToSnakeBody = member(MassExplosion, MassSnakeBody, parameter.DrainDeflectAngleVar, parameter.CombatHitFlashDuration)
	ExplosionToEye       = member(MassExplosion, MassEye, parameter.DrainDeflectAngleVar, parameter.CombatHitFlashDuration)
)

// --- Dust ---
// Dust impulses are accumulated per tick by DustSystem and applied in bulk;
// no immunity window (the accumulator provides the rate limit).

var (
	DustToDrain     = kinetic(MassDust, MassDrain, physics.ImpulseAdditive, parameter.DrainDeflectAngleVar, 0, 0)
	DustToComposite = kinetic(MassDust, MassQuasar, physics.ImpulseAdditive, parameter.DrainDeflectAngleVar, 0, 0)
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
