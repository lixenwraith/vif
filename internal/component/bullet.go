package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// BulletComponent marks a linear projectile entity with contact damage
type BulletComponent struct {
	Owner       core.Entity   // Source entity (telemetry, future filtering)
	Lifetime    time.Duration // Accumulated age
	MaxLifetime time.Duration // Destruction threshold
	Damage      CursorDamage
}
