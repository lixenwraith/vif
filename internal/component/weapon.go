package component

import (
	"time"
)

// WeaponType - rod (lightning - direct), launcher (missile - area), spray (acid - area dot)
type WeaponType int

const (
	WeaponRod WeaponType = iota
	WeaponLauncher
	WeaponDisruptor
	WeaponCount
)

// WeaponComponent tracks cursor weapon charges and orbiting state
// Charges[wt] == 0 means weapon not owned; availability derives from Charges, no separate flag
type WeaponComponent struct {
	Charges          [WeaponCount]int
	Cooldown         [WeaponCount]time.Duration
	MainFireCooldown time.Duration
}
