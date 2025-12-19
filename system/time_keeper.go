package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// TimeKeeperSystem manages lifecycle timers for entities
// It runs before cleanup to tag expired entities for destruction
type TimeKeeperSystem struct {
	world *engine.World
	res   engine.CoreResources

	timerStore *engine.Store[component.TimerComponent]
	deathStore *engine.Store[component.DeathComponent]
}

// NewTimeKeeperSystem creates a new timekeeper system
func NewTimeKeeperSystem(world *engine.World) engine.System {
	return &TimeKeeperSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		timerStore: engine.GetStore[component.TimerComponent](world),
		deathStore: engine.GetStore[component.DeathComponent](world),
	}
}

// Init
func (s *TimeKeeperSystem) Init() {}

// Priority returns the system's priority (runs just before CullSystem)
func (s *TimeKeeperSystem) Priority() int {
	return constant.PriorityTimekeeper
}

// EventTypes returns the event types TimeKeeperSystem handles
func (s *TimeKeeperSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventTimerStart,
	}
}

// HandleEvent processes timer registration events
func (s *TimeKeeperSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventTimerStart {
		if payload, ok := ev.Payload.(*event.TimerStartPayload); ok {
			s.timerStore.Add(payload.Entity, component.TimerComponent{
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
			s.deathStore.Add(entity, component.DeathComponent{})
		} else {
			s.timerStore.Add(entity, timer)
		}
	}
}