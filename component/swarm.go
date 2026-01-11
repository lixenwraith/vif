package component

import (
	"time"
)

// SwarmComponent holds swarm-specific runtime state, composite structure managed via HeaderComponent
type SwarmComponent struct {
	KineticState // PreciseX/Y, VelX/Y, AccelX/Y (Q32.32)

	IsEnraged bool // True if enraged after being hit

	// HP
	HitPoints         int
	HitFlashRemaining time.Duration
}