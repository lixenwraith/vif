package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
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
	CombatEntityCount
)

// Damage Types
type CombatDamageType int

const (
	CombatDamageNone CombatDamageType = iota
	CombatDamageDirect
	CombatDamageArea
	CombatDamageOverTime // Future
)

// Attack Types
type CombatAttackType int

const (
	CombatAttackProjectile CombatAttackType = iota
	CombatAttackShield
	CombatAttackLightning
	CombatAttackExplosion
	CombatAttackMissile
	CombatAttackPulse
	CombatAttackSelfDestruct
	CombatAttackTypeCount
)

// Effect Types
type CombatEffectMask uint64

const CombatEffectNone CombatEffectMask = 0
const (
	CombatEffectVampireDrain CombatEffectMask = 1 << iota
	CombatEffectKinetic
	CombatEffectStun // Future
)

// CombatComponent tags an entity to be identified as enemy for interactions
type CombatComponent struct {
	// OwnerEntity indicates owner/parent of the entity with combat component (e.g. cursor is the parent of cleaner)
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

	// RemainingHitFlash is the remaining duration of hit visual feedback
	RemainingHitFlash time.Duration

	// RemainingKineticImmunity is remaining immunity time for collision knockback
	RemainingKineticImmunity time.Duration

	// StunnedRemaining is remaining stun duration (movement suppressed)
	StunnedRemaining time.Duration
}
