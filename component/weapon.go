package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// WeaponType: rod (lightning - direct), launcher (missile - area), spray (acid - area dot)
type WeaponType int

const (
	WeaponRod WeaponType = iota
	WeaponLauncher
	WeaponSpray
)

// WeaponComponent tracks cursor active weapons
type WeaponComponent struct {
	MainFireCooldown time.Duration
	Active           map[WeaponType]bool
	Cooldown         map[WeaponType]time.Duration
	Orbs             map[WeaponType]core.Entity // Orb entity for each active weapon
}