package component

import (
	"time"
)

// CombatComponent tags an entity to be identified as enemy for interactions
type CombatComponent struct {
	// HitPoints is the remaining hit points of the combat entity (>0)
	HitPoints int

	// HitFlashRemaining is the remaining duration of hit visual feedback
	HitFlashRemaining time.Duration

	// KnockbackImmunityRemaining is remaining immunity time for collision knockback
	KnockbackImmunityRemaining time.Duration

	// DamageImmunityRemaining is remaining immunity time for damage
	DamageImmunityRemaining time.Duration
}