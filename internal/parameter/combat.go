package parameter

import (
	"time"
)

// Hit Points
const (
	// CombatInitialHPQuasar is quasar starting hit points
	CombatInitialHPQuasar = 100

	// CombatInitialHPDrain is drain starting hit points
	CombatInitialHPDrain = 10

	// CombatInitialHPSwarm is swarm starting hit points
	CombatInitialHPSwarm = 20

	// CombatInitialHPStorm is drain starting hit points
	CombatInitialHPStorm = 1000

	// CombatDamageEyeSelfDestruct is damage applied to target on eye self-destruct
	CombatDamageEyeSelfDestruct = 5
)

// Damage
const (
	// CombatDamageCleaner is damage caused by cleaner hit
	CombatDamageCleaner = 1

	// CombatDamageRod is damage caused by lightning rod buff hit
	CombatDamageRod = 1

	// CombatDamageExplosion is damage caused by each explosion center hit
	CombatDamageExplosion = 1

	// CombatDamageMissile is damage per missile impact
	CombatDamageMissile = 2

	// CombatDamagePulse is damage per pulse stun hit
	CombatDamagePulse = 1
)

// Timers
const (
	// CombatKineticImmunityDuration is the duration of immunity from homing/drag after collision
	CombatKineticImmunityDuration = 350 * time.Millisecond

	// CombatDamageImmunityDuration is the duration of immunity from damage after taking damage
	CombatDamageImmunityDuration = 150 * time.Millisecond

	// CombatHitFlashDuration is yellow flash duration and immunity window on cleaner hit
	CombatHitFlashDuration = 150 * time.Millisecond
)

// Kinetic Collision Impulse
const (
	// CollisionKineticImpulseMin is minimum knockback velocity (cells/sec)
	CollisionKineticImpulseMin = 15.0

	// CollisionKineticImpulseMax is maximum knockback velocity (cells/sec)
	CollisionKineticImpulseMax = 40.0
)

// Soft collision parameters (inter-species repulsion)
const (
	// SoftCollisionImmunityDuration is immunity window after soft repulsion
	SoftCollisionImmunityDuration = 100 * time.Millisecond

	// SoftCollisionImpulseMin is minimum repulsion velocity (cells/sec)
	SoftCollisionImpulseMin = 5.0

	// SoftCollisionImpulseMax is maximum repulsion velocity (cells/sec)
	SoftCollisionImpulseMax = 16.0

	// SoftCollisionAngleVar is random angle spread (radians, ~8°)
	SoftCollisionAngleVar = 0.15
)

// Swarm flocking separation parameters
const (
	// SwarmSeparationStrength is separation acceleration (cells/sec²)
	SwarmSeparationStrength = 3.0

	// SwarmQuasarSeparationWeight is weight multiplier for quasar in separation calc
	SwarmQuasarSeparationWeight = 0.3
)

// Entity collision radii (ellipse semi-axes from center)
const (
	// QuasarCollisionRadiusX is quasar horizontal collision radius (5/2 cells)
	QuasarCollisionRadiusX = 2.5

	// QuasarCollisionRadiusY is quasar vertical collision radius (3/2 cells)
	QuasarCollisionRadiusY = 1.5

	// SwarmCollisionRadiusX is swarm horizontal collision radius (4/2 cells)
	SwarmCollisionRadiusX = 2.0

	// SwarmCollisionRadiusY is swarm vertical collision radius (2/2 cells)
	SwarmCollisionRadiusY = 1.0

	// DrainCollisionRadius is drain collision radius (point entity with small area)
	DrainCollisionRadius = 0.5
)

// Vampire Drain
const (
	// VampireDrainEnergyValue is the amount of energy absorbed energy on hit
	VampireDrainEnergyValue = 100
)

// Missile
const (
	// MissileImpactRadius is hit detection threshold (cells)
	MissileImpactRadius = 1.5
)

// Pulse
const (
	// PulseStunDuration is the duration of disruptor weapon stun effect
	PulseStunDuration = 2000 * time.Millisecond

	// PulseRadiusX is disruptor weapon horizontal radius (2× shield)
	PulseRadiusX = PlayerShieldRadiusX * 3.5

	// PulseRadiusY is disruptor weapon vertical radius (2× shield, aspect corrected)
	PulseRadiusY = PlayerShieldRadiusY * 3.5

	// PulseEffectDuration is pulse visual effect duration
	PulseEffectDuration = 250 * time.Millisecond
)
