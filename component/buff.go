package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/core"
)

// BuffType: rod (lightning), launcher (missile), chain (pull)
type BuffType int

const (
	BuffRod BuffType = iota
	BuffLauncher
	BuffChain
)

// BuffComponent tracks cursor active buffs
type BuffComponent struct {
	MainFireCooldown time.Duration
	Active           map[BuffType]bool
	Cooldown         map[BuffType]time.Duration
	Orbs             map[BuffType]core.Entity // Orb entity for each active buff
}