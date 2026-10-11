package profile

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// Offset blend; mirrors collision.go
const (
	OffsetNone float64 = 0
	OffsetBody float64 = 1.0 / 3.0
)

// kinetic builds a combat knockback profile
func kinetic(impactor, target Mass, mode physics.ImpulseMode, angleVar, offset float64) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       massRatio(impactor, target),
		ImpulseMin:      parameter.CollisionKineticImpulseMin,
		ImpulseMax:      parameter.CollisionKineticImpulseMax,
		AngleVariance:   angleVar,
		Mode:            mode,
		OffsetInfluence: offset,
	}
}

// redirect sends the target off as a unit (tracer, shield)
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
		ImpulseMin:      parameter.SoftCollisionImpulseMin,
		ImpulseMax:      parameter.SoftCollisionImpulseMax,
		AngleVariance:   parameter.SoftCollisionAngleVar,
		Mode:            physics.ImpulseAdditive,
		OffsetInfluence: 0,
	}
}

// --- Tracer ---
var (
	TracerToDrain     = accumulate(MassTracer, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
	TracerToSwarm     = redirect(MassTracer, MassSwarm, parameter.SwarmDeflectAngleVar, OffsetBody)
	TracerToQuasar    = redirect(MassTracer, MassQuasar, parameter.DrainDeflectAngleVar, OffsetBody)
	TracerToStorm     = redirect(MassTracer, MassStorm, parameter.DrainDeflectAngleVar, OffsetBody)
	TracerToSnakeHead = redirect(MassTracer, MassSnakeHead, parameter.DrainDeflectAngleVar, OffsetBody)
	TracerToSnakeBody = accumulate(MassTracer, MassSnakeBody, parameter.DrainDeflectAngleVar, OffsetNone)
	TracerToEye       = redirect(MassTracer, MassEye, parameter.DrainDeflectAngleVar, OffsetBody)
)

// --- Shield ---
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
var (
	DustToDrain = accumulate(MassDust, MassDrain, parameter.DrainDeflectAngleVar, OffsetNone)
)

// --- Soft collision (inter-species scatter) ---
var (
	SoftSwarmToSwarm   = soft(MassSwarm, MassSwarm)
	SoftSwarmToQuasar  = soft(MassSwarm, MassQuasar)
	SoftQuasarToSwarm  = soft(MassQuasar, MassSwarm)
	SoftQuasarToQuasar = soft(MassQuasar, MassQuasar)
	SoftStormToSwarm   = soft(MassStorm, MassSwarm)
	SoftStormToQuasar  = soft(MassStorm, MassQuasar)
	SoftPylonToSwarm   = soft(MassPylon, MassSwarm)
	SoftPylonToQuasar  = soft(MassPylon, MassQuasar)
)

// Kraken pushes movable species but never receives a reciprocal impulse.
var SoftKraken = [component.SpeciesCount]physics.CollisionProfile{
	component.SpeciesDrain:  soft(MassKraken, MassDrain),
	component.SpeciesSwarm:  soft(MassKraken, MassSwarm),
	component.SpeciesQuasar: soft(MassKraken, MassQuasar),
	component.SpeciesStorm:  soft(MassKraken, MassStorm),
	component.SpeciesSnake:  soft(MassKraken, MassSnakeHead),
	component.SpeciesEye:    soft(MassKraken, MassEye),
}
