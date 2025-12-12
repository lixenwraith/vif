// @lixen: #focus{lifecycle[timer,keeper],event[dispatch]}
// @lixen: #interact{state[timer,time],end[entity],trigger[timer]}
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// TimeKeeperSystem manages lifecycle timers for entities
// It runs before cleanup to tag expired entities for destruction
type TimeKeeperSystem struct {
	ctx *engine.GameContext
}

// NewTimeKeeperSystem creates a new timekeeper system
func NewTimeKeeperSystem(ctx *engine.GameContext) *TimeKeeperSystem {
	return &TimeKeeperSystem{ctx: ctx}
}

// Priority returns the system's priority (runs just before CullSystem)
func (s *TimeKeeperSystem) Priority() int {
	return constants.PriorityTimekeeper
}

// EventTypes returns the event types TimeKeeperSystem handles
func (s *TimeKeeperSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventTimerStart,
	}
}

// HandleEvent processes timer registration events
func (s *TimeKeeperSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventTimerStart {
		if payload, ok := event.Payload.(*events.TimerStartPayload); ok {
			world.Timers.Add(payload.Entity, components.TimerComponent{
				Remaining: payload.Duration,
			})
		}
	}
}

// Update decrements timers and handles expiration
func (s *TimeKeeperSystem) Update(world *engine.World, dt time.Duration) {
	entities := world.Timers.All()

	for _, entity := range entities {
		timer, ok := world.Timers.Get(entity)
		if !ok {
			continue
		}

		timer.Remaining -= dt

		if timer.Remaining <= 0 {
			// Timer expired - Default action is destruction
			world.Timers.Remove(entity)
			world.MarkedForDeaths.Add(entity, components.MarkedForDeathComponent{})
		} else {
			world.Timers.Add(entity, timer)
		}
	}
}