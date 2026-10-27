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

// Soft builds an inter-species repulsion profile (scatter, not combat)
func Soft(impactor, target Mass) physics.CollisionProfile {
	return physics.CollisionProfile{
		MassRatio:       softRatio(impactor, target),
		ImpulseMin:      parameter.SoftCollisionImpulseMin,
		ImpulseMax:      parameter.SoftCollisionImpulseMax,
		AngleVariance:   parameter.SoftCollisionAngleVar,
		Mode:            physics.ImpulseAdditive,
		OffsetInfluence: 0,
	}
}

// RowMass is the target mass each combat row's knockback profiles are built with
var RowMass = [component.CombatEntityCount]Mass{
	component.CombatEntityDrain:     MassDrain,
	component.CombatEntityQuasar:    MassQuasar,
	component.CombatEntitySwarm:     MassSwarm,
	component.CombatEntityStorm:     MassStorm,
	component.CombatEntitySnakeHead: MassSnakeHead,
	component.CombatEntitySnakeBody: MassSnakeBody,
	component.CombatEntityEye:       MassEye,
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
	SoftSwarmToSwarm   = Soft(MassSwarm, MassSwarm)
	SoftSwarmToQuasar  = Soft(MassSwarm, MassQuasar)
	SoftQuasarToSwarm  = Soft(MassQuasar, MassSwarm)
	SoftQuasarToQuasar = Soft(MassQuasar, MassQuasar)
	SoftStormToSwarm   = Soft(MassStorm, MassSwarm)
	SoftStormToQuasar  = Soft(MassStorm, MassQuasar)
	SoftPylonToSwarm   = Soft(MassPylon, MassSwarm)
	SoftPylonToQuasar  = Soft(MassPylon, MassQuasar)
)

// Kraken pushes movable species but never receives a reciprocal impulse; a data
// species' push is built from its prototype's mass
var SoftKraken = [component.SpeciesMax]physics.CollisionProfile{
	component.SpeciesDrain:  Soft(MassKraken, MassDrain),
	component.SpeciesSwarm:  Soft(MassKraken, MassSwarm),
	component.SpeciesQuasar: Soft(MassKraken, MassQuasar),
	component.SpeciesStorm:  Soft(MassKraken, MassStorm),
	component.SpeciesSnake:  Soft(MassKraken, MassSnakeHead),
	component.SpeciesEye:    Soft(MassKraken, MassEye),
}
