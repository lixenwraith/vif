package component

import (
	"time"
)

// HeatComponent tracks the heat state
type HeatComponent struct {
	Current  int
	Overheat int

	// Ember state; EmberElapsed is the time since it lit or last burned heat
	EmberActive  bool
	EmberElapsed time.Duration
}
