package physics

import (
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Entity mass constants (Q32.32, relative units)
// Baseline: single-cell entity = Scale (1.0)
const (
	MassDust      = vmath.Scale / 10
	MassCursor    = vmath.Scale
	MassCleaner   = vmath.Scale
	MassExplosion = vmath.Scale * 10

	MassDrain     = vmath.Scale
	MassSwarm     = vmath.Scale * 2
	MassQuasar    = vmath.Scale * 10
	MassStorm     = vmath.Scale * 100
	MassPylon     = vmath.Scale * 1000 // Immovable
	MassSnakeHead = vmath.Scale * 8    // Similar to quasar
	MassSnakeBody = vmath.Scale * 2    // Per-segment, similar to swarm

	MassRatioEqual = vmath.Scale
)

// OffsetInfluenceDefault is standard blend factor for offset-based impulse
// Scale/3 ≈ 0.33 - offset contributes 1/3 to final direction
const OffsetInfluenceDefault = vmath.Scale / 3

// Collision profiles - pre-defined for zero allocation in hot path

// CleanerToDrain defines cleaner-to-drain collision (equal mass, additive)
var CleanerToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassCleaner, MassDrain),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// CleanerToQuasar defines cleaner-to-quasar collision (override for stun)
var CleanerToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassCleaner, MassQuasar),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// CleanerToSnakeHead defines cleaner-to-snake-head collision (override for unit composite)
var CleanerToSnakeHead = CollisionProfile{
	MassRatio:        vmath.Div(MassCleaner, MassSnakeHead),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// CleanerToSnakeBody defines cleaner-to-snake-body collision (additive for segments)
var CleanerToSnakeBody = CollisionProfile{
	MassRatio:        vmath.Div(MassCleaner, MassSnakeBody),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// ShieldToDrain defines shield-to-drain knockback (radial repulsion)
var ShieldToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassCursor, MassDrain),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// ShieldToQuasar defines shield-to-quasar knockback (centroid-based)
var ShieldToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassCursor, MassQuasar),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// ShieldToSnakeHead defines shield-to-snake-head knockback
var ShieldToSnakeHead = CollisionProfile{
	MassRatio:        vmath.Div(MassCursor, MassSnakeHead),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// ShieldToSnakeBody defines shield-to-snake-body knockback
var ShieldToSnakeBody = CollisionProfile{
	MassRatio:        vmath.Div(MassCursor, MassSnakeBody),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// TODO: unused
// DustToDrain defines dust-to-drain collision (light impactor, cumulative)
var DustToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassDust, MassDrain),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  0,
}

// ExplosionToDrain defines explosion-to-drain collision (severe impactor, cumulative)
var ExplosionToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassExplosion, MassDrain),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration, // Immunity for dedup
	OffsetInfluence:  0,
}

// ExplosionToQuasar defines explosion-to-drain collision (severe impactor, cumulative)
var ExplosionToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassExplosion, MassQuasar),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration, // Immunity for dedup
	OffsetInfluence:  0,
}

// ExplosionToSnakeHead defines explosion-to-snake-head collision
var ExplosionToSnakeHead = CollisionProfile{
	MassRatio:        vmath.Div(MassExplosion, MassSnakeHead),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// ExplosionToSnakeBody defines explosion-to-snake-body collision
var ExplosionToSnakeBody = CollisionProfile{
	MassRatio:        vmath.Div(MassExplosion, MassSnakeBody),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration,
	OffsetInfluence:  0,
}

// TODO: unused
// DustToQuasar defines dust-to-quasar collision (very light impactor, cumulative)
var DustToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassDust, MassQuasar),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  0, // Center-of-mass collision, no offset
}

// CleanerToSwarm defines cleaner-to-swarm collision
var CleanerToSwarm = CollisionProfile{
	MassRatio:        vmath.Div(MassCleaner, MassSwarm),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// ShieldToSwarm defines shield-to-swarm knockback
var ShieldToSwarm = CollisionProfile{
	MassRatio:        vmath.Div(MassCursor, MassQuasar),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  OffsetInfluenceDefault,
}

// ExplosionToSwarm defines explosion-to-swarm collision
var ExplosionToSwarm = CollisionProfile{
	MassRatio:        vmath.Div(MassExplosion, MassSwarm),
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration,
	OffsetInfluence:  0,
}

// Soft-collision profiles

// NOTE: Unused due little effect to quasar from lower mass drains, if masses change, can be used in quasar system implementation
// SoftCollisionDrainToQuasar defines drain-to-quasar soft repulsion
var SoftCollisionDrainToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassDrain, MassQuasar),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionSwarmToSwarm defines swarm-to-swarm soft repulsion
var SoftCollisionSwarmToSwarm = CollisionProfile{
	MassRatio:        MassRatioEqual,
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionSwarmToQuasar defines swarm-to-quasar soft repulsion
var SoftCollisionSwarmToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassSwarm, MassQuasar),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionQuasarToSwarm defines quasar-to-swarm soft repulsion
var SoftCollisionQuasarToSwarm = CollisionProfile{
	MassRatio:        vmath.Div(MassQuasar, MassSwarm),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionQuasarToDrain defines quasar-to-drain soft repulsion
var SoftCollisionQuasarToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassQuasar, MassDrain),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionQuasarToQuasar defines quasar-to-quasar soft repulsion
var SoftCollisionQuasarToQuasar = CollisionProfile{
	MassRatio:        MassRatioEqual,
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionPylonToDrain defines pylon-to-drain soft repulsion
var SoftCollisionPylonToDrain = CollisionProfile{
	MassRatio:        vmath.Div(MassPylon, MassDrain),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionPylonToSwarm defines pylon-to-swarm soft repulsion
var SoftCollisionPylonToSwarm = CollisionProfile{
	MassRatio:        vmath.Div(MassPylon, MassSwarm),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionPylonToQuasar defines pylon-to-quasar soft repulsion
var SoftCollisionPylonToQuasar = CollisionProfile{
	MassRatio:        vmath.Div(MassPylon, MassQuasar),
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// Homing profiles

// DrainHoming defines drain entity homing behavior
var DrainHoming = HomingProfile{
	BaseSpeed:        parameter.DrainBaseSpeed,
	HomingAccel:      parameter.DrainHomingAccel,
	Drag:             parameter.DrainDrag,
	ArrivalRadius:    0, // No arrival steering
	ArrivalDragBoost: 0,
	DeadZone:         0, // Use default settling
}

// QuasarHoming defines quasar entity homing behavior with arrival steering
var QuasarHoming = HomingProfile{
	BaseSpeed:        parameter.QuasarBaseSpeed,
	HomingAccel:      parameter.QuasarHomingAccel,
	Drag:             parameter.QuasarDrag,
	ArrivalRadius:    vmath.FromFloat(3.0), // Begin arrival steering at 3 cells
	ArrivalDragBoost: vmath.FromFloat(3.0), // 4x drag at target (1 + 3)
	DeadZone:         vmath.Scale / 2,      // Snap at 0.5 cells
}

// SwarmHoming defines swarm chase behavior (4x drain speed)
var SwarmHoming = HomingProfile{
	BaseSpeed:        parameter.SwarmChaseSpeed,
	HomingAccel:      parameter.SwarmHomingAccel,
	Drag:             parameter.SwarmDrag,
	ArrivalRadius:    0,
	ArrivalDragBoost: 0,
	DeadZone:         0,
}

// LootHoming defines loot attraction behavior
// Uses aggressive arrival drag to kill orbital momentum and ensure cursor capture
var LootHoming = HomingProfile{
	BaseSpeed:        parameter.LootChaseSpeed,
	HomingAccel:      parameter.LootHomingAccel,
	Drag:             vmath.FromFloat(2.0),  // Low base drag
	ArrivalRadius:    vmath.FromFloat(5.0),  // Start braking 5 cells away
	ArrivalDragBoost: vmath.FromFloat(25.0), // Massive drag near target
	DeadZone:         vmath.Scale / 2,       // Snap at 0.5 cells
}

// MissileSeekerHoming: continuous drag, maintains full accel in arrival zone
var MissileSeekerHoming = HomingProfile{
	BaseSpeed:        0, // Continuous drag (overspeed always true)
	HomingAccel:      parameter.MissileSeekerHomingAccel,
	Drag:             parameter.MissileSeekerDrag,
	ArrivalRadius:    parameter.MissileSeekerArrivalRadius,
	ArrivalDragBoost: vmath.FromFloat(2.0), // 3x drag at target
	ArrivalAccelMin:  vmath.Scale,          // Maintain full accel
	DeadZone:         vmath.Scale / 10,     // Very small; impact check handles arrival
}

// SnakeHoming defines snake head homing behavior
var SnakeHoming = HomingProfile{
	BaseSpeed:        parameter.SnakeBaseSpeed,
	HomingAccel:      parameter.SnakeHomingAccel,
	Drag:             parameter.SnakeDrag,
	ArrivalRadius:    vmath.FromFloat(2.0),
	ArrivalDragBoost: vmath.FromFloat(2.0),
	DeadZone:         vmath.Scale / 2,
}