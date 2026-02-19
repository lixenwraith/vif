package component

import (
	"time"
)

// HeatComponent tracks the heat state
type HeatComponent struct {
	Current             int
	Overheat            int
	BurstFlashRemaining time.Duration

	// Ember state
	EmberActive    bool
	EmberDecayTime time.Time
}