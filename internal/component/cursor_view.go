package component

import (
	"time"
)

// CursorViewComponent holds one cursor's local presentation state. Attached to the
// shared cursor entity, but owned and written by this instance alone (D-13).
//
// Every field is a value. The component used to carry an `Orbs` array of
// player-domain entity handles, and a handle is meaningful only inside the
// instance that allocated it: the live cursor-state sync excluded the array by
// hand, but a shared capture serialises whole components, so a correction handed a
// receiver another instance's zeroes and orphaned the orbs its own handles named.
// The index lives in `WeaponSystem` now, derived from the `Orb` store — which is
// the only record of a live orb — so no shared entity carries a player-domain
// reference and there is nothing left for a capture to leak (D-4, D-13).
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
}
