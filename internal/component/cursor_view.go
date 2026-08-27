package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

// CursorViewComponent holds one cursor's local presentation state and player-domain
// references. Attached to the shared cursor entity, but owned by this instance alone.
type CursorViewComponent struct {
	// ErrorFlashRemaining is the duration remaining for the error flash
	ErrorFlashRemaining time.Duration

	// BurstFlashRemaining is the duration remaining for the overheat burst flash
	BurstFlashRemaining time.Duration

	// Energy blink state
	BlinkActive    bool
	BlinkType      int // 0=error, 1=blue, 2=green, 3=red, 4=gold
	BlinkLevel     int // 0=dark, 1=normal, 2=bright
	BlinkRemaining time.Duration

	// Orbs indexes this cursor's orb entities by weapon type, 0 = none
	Orbs [WeaponCount]core.Entity
}
