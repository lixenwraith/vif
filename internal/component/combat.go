package component

import (
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
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
	CombatAttackBullet
	CombatAttackRay
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

	// StunnedRemaining is remaining stun duration (movement suppressed)
	StunnedRemaining time.Duration
}

// unownedAttacker is the immunity bit for an attack no cursor owns
const unownedAttacker = 1 << parameter.MaxPlayers

func (c *CombatComponent) DamageImmuneTo(attacker uint32, weapon CombatAttackType) bool {
	if uint(weapon) >= uint(CombatAttackTypeCount) {
		return true
	}
	return c.RemainingDamageImmunity != 0 && c.DamageImmunitySpent[weapon]&attacker != 0
}

// SpendDamageImmunity records a landed hit, opening the window when it is closed.
// An attacker joining a window late may land twice inside one duration; the rate
// stays bounded at two hits per window and the alternative is a timer per slot.
func (c *CombatComponent) SpendDamageImmunity(attacker uint32, weapon CombatAttackType, d time.Duration) {
	if uint(weapon) >= uint(CombatAttackTypeCount) {
		return
	}
	if c.RemainingDamageImmunity == 0 {
		c.RemainingDamageImmunity = d
		clear(c.DamageImmunitySpent[:])
	}
	c.DamageImmunitySpent[weapon] |= attacker
}

// SealDamageImmunity opens a window no attacker may spend, for species-authored
// invulnerability rather than the per-attacker hit rate limit.
func (c *CombatComponent) SealDamageImmunity(d time.Duration) {
	c.RemainingDamageImmunity = d
	for weapon := range c.DamageImmunitySpent {
		c.DamageImmunitySpent[weapon] = ^uint32(0)
	}
}

// Every weapon and player shares the target's displacement window.
func (c *CombatComponent) KineticImmuneTo(_ uint32) bool {
	return c.RemainingKineticImmunity != 0
}

// Record the accepted source without extending an open displacement window.
// The return value preserves the impulse replacement/composition contract.
func (c *CombatComponent) SpendKineticImmunity(attacker uint32, d time.Duration) (opened bool) {
	if c.RemainingKineticImmunity == 0 {
		c.RemainingKineticImmunity = d
		c.KineticImmunitySpent = 0
		opened = true
	}
	c.KineticImmunitySpent |= attacker
	return opened
}

// SealKineticImmunity opens a displacement window no attacker may spend, for the
// species-authored knockback that has no attacker to name.
func (c *CombatComponent) SealKineticImmunity(d time.Duration) {
	c.RemainingKineticImmunity = d
	c.KineticImmunitySpent = ^uint32(0)
}

// AttackerBit names an attacking cursor's slot inside a target's immunity window.
func AttackerBit(slot uint8, owned bool) uint32 {
	if owned && int(slot) < parameter.MaxPlayers {
		return 1 << slot
	}
	return unownedAttacker
}
