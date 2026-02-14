package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// BulletDamage defines contact damage for a bullet on cursor interaction
type BulletDamage struct {
	EnergyDrain int // Shield drain amount on shield contact
	HeatDelta   int // Heat change on direct cursor hit (negative = reduce)
}

// BulletComponent marks a linear projectile entity with contact damage
type BulletComponent struct {
	Owner       core.Entity   // Source entity (telemetry, future filtering)
	Lifetime    time.Duration // Accumulated age
	MaxLifetime time.Duration // Destruction threshold
	Damage      BulletDamage
}