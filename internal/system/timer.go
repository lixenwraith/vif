package system

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TimerSystem manages lifecycle timers for entities
// It runs before cleanup to tag expired entities for destruction
type TimerSystem struct {
	world *engine.World

	enabled bool
}

// NewTimerSystem creates a new timer system
func NewTimerSystem(world *engine.World) engine.System {
	s := &TimerSystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *TimerSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *TimerSystem) Name() string {
	return "timekeeper"
}

// Domain reports dual: it expires timers on entities of either domain.
func (s *TimerSystem) Domain() engine.SystemDomain { return engine.SystemDual }

// Priority returns the system's priority (runs just before CullSystem)
func (s *TimerSystem) Priority() int {
	return parameter.PriorityTimekeeper
}

// EventTypes returns the event types TimerSystem handles
func (s *TimerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventTimerStart,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes timer registration events
func (s *TimerSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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
func (s *TimerSystem) Update() {
	if !s.enabled {
		return
	}

	timers := s.world.Components.Timer
	dt := s.world.Resources.Time.DeltaTime
	var expired []core.Entity

	for _, entity := range timers.Entities() {
		timer, ok := timers.GetPtr(entity)
		if !ok {
			continue
		}

		timer.Remaining -= dt

		if timer.Remaining <= 0 {
			s.world.Components.Death.SetComponent(entity, component.DeathComponent{})
			expired = append(expired, entity)
		}
	}

	timers.RemoveBatch(expired)
}
