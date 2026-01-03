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
	engine.SystemBase

	enabled bool
}

// NewTimeKeeperSystem creates a new timekeeper system
func NewTimeKeeperSystem(world *engine.World) engine.System {
	s := &TimeKeeperSystem{
		SystemBase: engine.NewSystemBase(world),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *TimeKeeperSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *TimeKeeperSystem) initLocked() {
	s.enabled = true
}

// Priority returns the system's priority (runs just before CullSystem)
func (s *TimeKeeperSystem) Priority() int {
	return constant.PriorityTimekeeper
}

// EventTypes returns the event types TimeKeeperSystem handles
func (s *TimeKeeperSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventTimerStart,
	}
}

// HandleEvent processes timer registration events
func (s *TimeKeeperSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventTimerStart {
		if payload, ok := ev.Payload.(*event.TimerStartPayload); ok {
			s.Component.Timer.Set(payload.Entity, component.TimerComponent{
				Remaining: payload.Duration,
			})
		}
	}
}

// Update decrements timers and handles expiration
func (s *TimeKeeperSystem) Update() {
	if !s.enabled {
		return
	}

	entities := s.Component.Timer.All()
	dt := s.Resource.Time.DeltaTime

	for _, entity := range entities {
		timer, ok := s.Component.Timer.Get(entity)
		if !ok {
			continue
		}

		timer.Remaining -= dt

		if timer.Remaining <= 0 {
			// Timer expired - Default action is destruction
			s.Component.Timer.Remove(entity)
			s.Component.Death.Set(entity, component.DeathComponent{})
		} else {
			s.Component.Timer.Set(entity, timer)
		}
	}
}