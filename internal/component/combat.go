package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
)

type CombatEntityType int

const (
	CombatEntityCursor CombatEntityType = iota
	CombatEntityDrain
	CombatEntityQuasar
	CombatEntitySwarm
	CombatEntityStorm
	CombatEntityPylon
	CombatEntitySnakeHead
	CombatEntitySnakeBody
	CombatEntityEye
	CombatEntityTower
	CombatEntityKraken
	CombatEntityCount
)

// Damage Types
type CombatDamageType int

const (
	CombatDamageNone CombatDamageType = iota
	CombatDamageDirect
	CombatDamageArea
	CombatDamageOverTime // a status the direct resolver leaves, ticking from the profile's figures
)

// Attack Types
type CombatAttackType int

// CombatAttackNone selects no attack profile; effects carrying it are visual only
const CombatAttackNone CombatAttackType = -1

const (
	CombatAttackTracer CombatAttackType = iota
	CombatAttackShield
	CombatAttackLightning
	CombatAttackExplosion
	CombatAttackMissile
	CombatAttackPulse
	CombatAttackSelfDestruct
	CombatAttackBullet
	CombatAttackRay
	CombatAttackBurn // a charged pulse's follow-up; nothing spends its immunity, so only a sealed window blocks it
	CombatAttackTypeCount
)

// Effect Types
type CombatEffectMask uint64

const CombatEffectNone CombatEffectMask = 0
const (
	CombatEffectEnergyDrain CombatEffectMask = 1 << iota
	CombatEffectKinetic
	CombatEffectStun
)

// StatusKind indexes CombatComponent.Status: one slot per kind, no stacking
type StatusKind uint8

const (
	StatusStun StatusKind = iota // movement suppressed
	StatusBurn                   // periodic damage credited to Source
	StatusCount
)

// Status is one effect a hit left on a combat entity, aged by combat each tick
type Status struct {
	Remaining time.Duration    // zero: inactive
	Period    time.Duration    // between ticks of a periodic effect; zero: none
	Timer     time.Duration    // until the next tick
	Magnitude int              // what a tick applies
	Source    core.Entity      // crediting cursor; Shared, so capture-safe
	Attack    CombatAttackType // family a tick records under and checks immunity against
}

// CombatComponent tags an entity as combat-relevant for interactions.
type CombatComponent struct {
	// OwnerEntity indicates owner/parent of the entity with combat component (e.g. cursor is the parent of a tracer)
	OwnerEntity core.Entity

	// LastDamagedBy identifies the cursor that most recently dealt HP damage.
	// Zero means the last damaging attack was not owned by a live cursor.
	LastDamagedBy core.Entity

	// CombatEntityType
	CombatEntityType CombatEntityType

	// HitPoints is the remaining hit points of the combat entity (>0)
	HitPoints int

	// IsEnraged is the enrage indicator that modifies combat behavior
	IsEnraged bool

	// RemainingDamageImmunity is remaining immunity time for damage
	RemainingDamageImmunity time.Duration

	// Each attack family has a separate allowance per cursor in the open window.
	DamageImmunitySpent [CombatAttackTypeCount]uint32

	// RemainingHitFlash is the remaining duration of hit visual feedback
	RemainingHitFlash time.Duration

	// RemainingKineticImmunity is remaining immunity time for collision knockback.
	// It is also how every steering system asks "is this target currently
	// displaced", so it stays the whole target's window whoever opened it.
	RemainingKineticImmunity time.Duration

	// Records accepted displacement sources; immunity itself covers every attacker.
	KineticImmunitySpent uint32

	// Status is indexed by StatusKind
	Status [StatusCount]Status
}
