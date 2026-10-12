package component

import "github.com/lixenwraith/vif/internal/core"

// BulletComponent marks a linear projectile entity with contact damage; its
// lifetime timer ends it
type BulletComponent struct {
	Owner core.Entity // Firing cursor, or a mount's Shared host

	// Hostile bullets strike cursors for Damage; a cursor's resolve against species as Attack
	Hostile bool
	Damage  CursorDamage
	Attack  CombatAttackType
}
