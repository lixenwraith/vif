package constant

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

// --- Shield Knockback ---
const (
	// ShieldKnockbackImpulseMinFloat is minimum knockback velocity (cells/sec)
	ShieldKnockbackImpulseMinFloat = 15.0

	// ShieldKnockbackImpulseMaxFloat is maximum knockback velocity (cells/sec)
	ShieldKnockbackImpulseMaxFloat = 40.0
)

