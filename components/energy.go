package components

import (
	"sync/atomic"
)

// EnergyComponent holds the energy state and visual blink state
// Previously part of global GameState, now attached to the cursor entity
type EnergyComponent struct {
	Current        atomic.Int64
	BlinkActive    atomic.Bool
	BlinkType      atomic.Uint32 // 0=error, 1=blue, 2=green, 3=red, 4=gold
	BlinkLevel     atomic.Uint32 // 0=dark, 1=normal, 2=bright
	BlinkRemaining atomic.Int64  // Nanoseconds remaining for blink
}