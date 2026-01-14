package component

import (
	"time"
)

// EnergyComponent holds the energy state and visual blink state
type EnergyComponent struct {
	Current        int64
	BlinkActive    bool
	BlinkType      int           // 0=error, 1=blue, 2=green, 3=red, 4=gold
	BlinkLevel     int           // 0=dark, 1=normal, 2=bright
	BlinkRemaining time.Duration // Nanoseconds remaining for blink
}