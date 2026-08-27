package component

import (
	"time"
)

// HeatComponent tracks the heat state
type HeatComponent struct {
	Current  int
	Overheat int

	// Ember state
	EmberActive    bool
	EmberDecayTime time.Time
}
