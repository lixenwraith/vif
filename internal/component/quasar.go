package component

import (
	"time"
)

// QuasarComponent holds quasar-specific runtime state, composite structure managed via HeaderComponent
type QuasarComponent struct {
	LastSpeedIncreaseAt time.Time // For periodic speed scaling

	SpeedMultiplier float64 // Current speed scale factor (starts at 1.0)

	// Quasar state
	IsZapping  bool // True if zapping cursor outside range
	IsCharging bool // True if charging to zap with cursor outside range
	IsShielded bool // True if shielded, indicates damage immunity, is in sync with quasar's shield component active state

	// Charge phase state (delay before zapping)
	ChargeRemaining time.Duration

	// Dynamic resize support
	ZapRadius float64 // Visual radius of zap circle in cells (dynamic on resize)
}
