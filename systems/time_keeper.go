package systems

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// TimeKeeperSystem manages lifecycle timers for entities
// It runs before cleanup to tag expired entities for destruction
type TimeKeeperSystem struct {
	world *engine.World
	res   engine.CoreResources

	timerStore *engine.Store[components.TimerComponent]
	deathStore *engine.Store[components.DeathComponent]
}

// NewTimeKeeperSystem creates a new timekeeper system
func NewTimeKeeperSystem(world *engine.World) engine.System {
	return &TimeKeeperSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		timerStore: engine.GetStore[components.TimerComponent](world),
		deathStore: engine.GetStore[components.DeathComponent](world),
	}
}

// Init
func (s *TimeKeeperSystem) Init() {}

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
func (s *TimeKeeperSystem) HandleEvent(event events.GameEvent) {
	if event.Type == events.EventTimerStart {
		if payload, ok := event.Payload.(*events.TimerStartPayload); ok {
			s.timerStore.Add(payload.Entity, components.TimerComponent{
				Remaining: payload.Duration,
			})
		}
	}
}

// Update decrements timers and handles expiration
func (s *TimeKeeperSystem) Update() {
	entities := s.timerStore.All()
	dt := s.res.Time.DeltaTime

	for _, entity := range entities {
		timer, ok := s.timerStore.Get(entity)
		if !ok {
			continue
		}

		timer.Remaining -= dt

		if timer.Remaining <= 0 {
			// Timer expired - Default action is destruction
			s.timerStore.Remove(entity)
			s.deathStore.Add(entity, components.DeathComponent{})
		} else {
			s.timerStore.Add(entity, timer)
		}
	}
}