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

	// RemainingKineticImmunity is remaining immunity time for collision knockback.
	// It is also how every steering system asks "is this target currently
	// displaced", so it stays the whole target's window whoever opened it.
	RemainingKineticImmunity time.Duration

	// KineticImmunitySpent names the attackers that have already landed a
	// knockback in the open window, one bit per roster slot, exactly as
	// DamageImmunitySpent does for damage.
	KineticImmunitySpent uint32

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

// KineticImmuneTo reports whether this attacker already spent its knockback in the
// open window. Per attacker for the reason damage is, and for a second one: a
// crossing applies at once on its producer and a playout lead later everywhere
// else, so a shared latch is taken by whichever participant's hit that instance
// applied first — its own — and the other's is discarded rather than delayed. Two
// participants hitting one swarm then sent it opposite ways on their two screens
// until the next correction.
func (c *CombatComponent) KineticImmuneTo(attacker uint32) bool {
	return c.RemainingKineticImmunity != 0 && c.KineticImmunitySpent&attacker != 0
}

// SpendKineticImmunity records a landed knockback, opening the window when it is
// closed, and reports whether this hit is the one that opened it. That answer is
// what decides override against additive: the opening hit replaces the target's
// velocity and every hit joining the window adds to it, so a window's impulses
// compose to the same vector whatever order an instance saw them in.
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
