package component

import (
	"time"
)

// HeatComponent tracks the heat state, and the burst meter a scenario spends, which
// heat gained and shield hits fill (parameter.BurstFull)
type HeatComponent struct {
	Current  int
	Overheat int
	Burst    int

	// Ember state; EmberElapsed is the time since it lit or last burned heat
	EmberActive  bool
	EmberElapsed time.Duration
}
