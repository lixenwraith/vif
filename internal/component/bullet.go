package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// BulletComponent marks a linear projectile entity with contact damage
type BulletComponent struct {
	Owner       core.Entity   // Firing cursor, or a mount's Shared host
	Lifetime    time.Duration // Accumulated age
	MaxLifetime time.Duration // Destruction threshold

	// Hostile bullets strike cursors for Damage; a cursor's resolve against species as Attack
	Hostile bool
	Damage  CursorDamage
	Attack  CombatAttackType
}
