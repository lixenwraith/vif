package component

import (
	"time"
)

// SwarmComponent holds swarm-specific runtime state, composite structure managed via HeaderComponent
type SwarmComponent struct {
	Kinetic // PreciseX/Y, VelX/Y, AccelX/Y (Q32.32)

	IsEnraged bool // True if enraged after being hit

	// HitPoints
	HitPoints         int
	HitFlashRemaining time.Duration
}