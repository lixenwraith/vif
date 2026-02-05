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
	// CollisionKineticImpulseMinFloat is minimum knockback velocity (cells/sec)
	CollisionKineticImpulseMinFloat = 15.0

	// CollisionKineticImpulseMaxFloat is maximum knockback velocity (cells/sec)
	CollisionKineticImpulseMaxFloat = 40.0
)

// Soft collision parameters (inter-enemy repulsion)
const (
	// SoftCollisionImmunityDuration is immunity window after soft repulsion
	SoftCollisionImmunityDuration = 100 * time.Millisecond

	// SoftCollisionImpulseMinFloat is minimum repulsion velocity (cells/sec)
	SoftCollisionImpulseMinFloat = 5.0

	// SoftCollisionImpulseMaxFloat is maximum repulsion velocity (cells/sec)
	SoftCollisionImpulseMaxFloat = 16.0

	// SoftCollisionAngleVarFloat is random angle spread (radians, ~8°)
	SoftCollisionAngleVarFloat = 0.15
)

// Swarm flocking separation parameters
const (
	// SwarmSeparationRadiusXFloat is horizontal separation zone (cells)
	SwarmSeparationRadiusXFloat = 8.0

	// SwarmSeparationRadiusYFloat is vertical separation zone (cells, aspect-corrected)
	SwarmSeparationRadiusYFloat = 4.0

	// SwarmSeparationStrengthFloat is separation acceleration (cells/sec²)
	SwarmSeparationStrengthFloat = 3.0

	// SwarmQuasarSeparationWeight is weight multiplier for quasar in separation calc
	SwarmQuasarSeparationWeight = 0.3
)

// Entity collision radii (ellipse semi-axes from center)
const (
	// QuasarCollisionRadiusXFloat is quasar horizontal collision radius (5/2 cells)
	QuasarCollisionRadiusXFloat = 2.5

	// QuasarCollisionRadiusYFloat is quasar vertical collision radius (3/2 cells)
	QuasarCollisionRadiusYFloat = 1.5

	// SwarmCollisionRadiusXFloat is swarm horizontal collision radius (4/2 cells)
	SwarmCollisionRadiusXFloat = 2.0

	// SwarmCollisionRadiusYFloat is swarm vertical collision radius (2/2 cells)
	SwarmCollisionRadiusYFloat = 1.0

	// DrainCollisionRadiusFloat is drain collision radius (point entity with small area)
	DrainCollisionRadiusFloat = 0.5
)

// Vampire Drain
const (
	// VampireDrainEnergyValue is the amount of energy absorbed energy on hit
	VampireDrainEnergyValue = 100
)

// Missile
const (
	// MissileImpactRadiusFloat is hit detection threshold (cells)
	MissileImpactRadiusFloat = 1.5
)