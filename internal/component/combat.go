package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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

// CombatAttackNone selects no attack profile; effects carrying it are visual only
const CombatAttackNone CombatAttackType = -1

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
	CombatEffectEnergyDrain CombatEffectMask = 1 << iota
	CombatEffectKinetic
	CombatEffectStun // Future
)

// CombatComponent tags an entity as combat-relevant for interactions.
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

	// DamageImmunitySpent names the attackers that have already landed a hit in
	// the open window: one bit per roster slot, the top bit for an attack no
	// cursor owns. The window is the target's, its budget is per attacker.
	DamageImmunitySpent uint32

	// RemainingHitFlash is the remaining duration of hit visual feedback
	RemainingHitFlash time.Duration

	// RemainingKineticImmunity is remaining immunity time for collision knockback
	RemainingKineticImmunity time.Duration

	// StunnedRemaining is remaining stun duration (movement suppressed)
	StunnedRemaining time.Duration
}

// unownedAttacker is the immunity bit for an attack no cursor owns
const unownedAttacker = 1 << parameter.MaxPlayers

// DamageImmuneTo reports whether this attacker already spent its hit in the open
// window. A window one participant opened does not consume another's budget: a
// shared cooldown would divide one target's damage between the roster.
func (c *CombatComponent) DamageImmuneTo(attacker uint32) bool {
	return c.RemainingDamageImmunity != 0 && c.DamageImmunitySpent&attacker != 0
}

// SpendDamageImmunity records a landed hit, opening the window when it is closed.
// An attacker joining a window late may land twice inside one duration; the rate
// stays bounded at two hits per window and the alternative is a timer per slot.
func (c *CombatComponent) SpendDamageImmunity(attacker uint32, d time.Duration) {
	if c.RemainingDamageImmunity == 0 {
		c.RemainingDamageImmunity = d
		c.DamageImmunitySpent = 0
	}
	c.DamageImmunitySpent |= attacker
}

// SealDamageImmunity opens a window no attacker may spend, for species-authored
// invulnerability rather than the per-attacker hit rate limit.
func (c *CombatComponent) SealDamageImmunity(d time.Duration) {
	c.RemainingDamageImmunity = d
	c.DamageImmunitySpent = ^uint32(0)
}

// AttackerBit names an attacking cursor's slot inside a target's immunity window.
func AttackerBit(slot uint8, owned bool) uint32 {
	if owned && int(slot) < parameter.MaxPlayers {
		return 1 << slot
	}
	return unownedAttacker
}
