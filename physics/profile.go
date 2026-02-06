package physics

import (
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Collision profiles - pre-defined for zero allocation in hot path

// CleanerToDrain defines cleaner-to-drain collision (equal mass, additive)
var CleanerToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// CleanerToQuasar defines cleaner-to-quasar collision (override for stun)
var CleanerToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioBaseToQuasar,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// ShieldToDrain defines shield-to-drain knockback (radial repulsion)
var ShieldToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  0,
}

// ShieldToQuasar defines shield-to-quasar knockback (centroid-based)
var ShieldToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioBaseToQuasar,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// DustToDrain defines dust-to-drain collision (light impactor, cumulative)
var DustToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioDustToDrain,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  0,
}

// ExplosionToDrain defines explosion-to-drain collision (severe impactor, cumulative)
var ExplosionToDrain = CollisionProfile{
	MassRatio:        vmath.MassRatioExplosionToDrain,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatKineticImmunityDuration, // Immunity for dedup
	OffsetInfluence:  0,
}

// ExplosionToQuasar defines explosion-to-drain collision (severe impactor, cumulative)
var ExplosionToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioExplosionToQuasar,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration, // Immunity for dedup
	OffsetInfluence:  0,
}

// DustToQuasar defines dust-to-quasar collision (very light impactor, cumulative)
var DustToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioDustToQuasar,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.DrainDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: 0, // No immunity - cumulative hits
	OffsetInfluence:  0, // Center-of-mass collision, no offset
}

// CleanerToSwarm defines cleaner-to-swarm collision
var CleanerToSwarm = CollisionProfile{
	MassRatio:        vmath.MassRatioBaseToSwarm, // Swarm is heavy like quasar
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// ShieldToSwarm defines shield-to-swarm knockback
var ShieldToSwarm = CollisionProfile{
	MassRatio:        vmath.MassRatioBaseToQuasar,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseOverride,
	ImmunityDuration: parameter.CombatKineticImmunityDuration,
	OffsetInfluence:  vmath.OffsetInfluenceDefault,
}

// ExplosionToSwarm defines explosion-to-swarm collision
var ExplosionToSwarm = CollisionProfile{
	MassRatio:        vmath.MassRatioExplosionToSwarm,
	ImpulseMin:       parameter.CollisionKineticImpulseMin,
	ImpulseMax:       parameter.CollisionKineticImpulseMax,
	AngleVariance:    parameter.SwarmDeflectAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.CombatHitFlashDuration,
	OffsetInfluence:  0,
}

// Soft-collision profiles

// SoftCollisionDrainToQuasar defines drain-to-quasar soft repulsion
var SoftCollisionDrainToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionSwarmToSwarm defines swarm-to-swarm soft repulsion
var SoftCollisionSwarmToSwarm = CollisionProfile{
	MassRatio:        vmath.MassRatioEqual,
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionSwarmToQuasar defines swarm-to-quasar soft repulsion
var SoftCollisionSwarmToQuasar = CollisionProfile{
	MassRatio:        vmath.MassRatioBaseToQuasar, // Quasar is heavier
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionQuasarToSwarm defines quasar-to-swarm soft repulsion
var SoftCollisionQuasarToSwarm = CollisionProfile{
	MassRatio:        vmath.Scale * 10, // Quasar pushes swarm harder
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// SoftCollisionQuasarToDrain defines quasar-to-drain soft repulsion
var SoftCollisionQuasarToDrain = CollisionProfile{
	MassRatio:        vmath.Scale * 10, // Quasar pushes drain harder
	ImpulseMin:       parameter.SoftCollisionImpulseMin,
	ImpulseMax:       parameter.SoftCollisionImpulseMax,
	AngleVariance:    parameter.SoftCollisionAngleVar,
	Mode:             ImpulseAdditive,
	ImmunityDuration: parameter.SoftCollisionImmunityDuration,
	OffsetInfluence:  0,
}

// Homing profiles

// TODO: wire in unused profiles

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
	BaseSpeed:        vmath.FromFloat(parameter.LootHomingMaxSpeed),
	HomingAccel:      vmath.FromFloat(parameter.LootHomingAccel),
	Drag:             vmath.FromFloat(2.0),  // Low base drag
	ArrivalRadius:    vmath.FromFloat(5.0),  // Start braking 5 cells away
	ArrivalDragBoost: vmath.FromFloat(25.0), // Massive drag near target
	DeadZone:         vmath.Scale / 2,       // Snap at 0.5 cells
}