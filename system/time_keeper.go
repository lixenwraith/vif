package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// TimeKeeperSystem manages lifecycle timers for entities
// It runs before cleanup to tag expired entities for destruction
type TimeKeeperSystem struct {
	world *engine.World

	enabled bool
}

// NewTimeKeeperSystem creates a new timekeeper system
func NewTimeKeeperSystem(world *engine.World) engine.System {
	s := &TimeKeeperSystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *TimeKeeperSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *TimeKeeperSystem) Name() string {
	return "timekeeper"
}

// Priority returns the system's priority (runs just before CullSystem)
func (s *TimeKeeperSystem) Priority() int {
	return parameter.PriorityTimekeeper
}

// EventTypes returns the event types TimeKeeperSystem handles
func (s *TimeKeeperSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventTimerStart,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes timer registration events
func (s *TimeKeeperSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventTimerStart {
		if payload, ok := ev.Payload.(*event.TimerStartPayload); ok {
			s.world.Components.Timer.SetComponent(payload.Entity, component.TimerComponent{
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

	entities := s.world.Components.Timer.GetAllEntities()
	dt := s.world.Resources.Time.DeltaTime

	for _, entity := range entities {
		timer, ok := s.world.Components.Timer.GetComponent(entity)
		if !ok {
			continue
		}

		timer.Remaining -= dt

		if timer.Remaining <= 0 {
			// Timer expired
			s.world.Components.Timer.RemoveEntity(entity)
			s.world.Components.Death.SetComponent(entity, component.DeathComponent{})
		} else {
			s.world.Components.Timer.SetComponent(entity, timer)
		}
	}
}