package component

import (
	"time"
)

// BoostComponent tracks the state of the player's boost ability
type BoostComponent struct {
	Active        bool
	Remaining     time.Duration
	TotalDuration time.Duration // For UI progress calculation
}