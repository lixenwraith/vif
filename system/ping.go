package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// PingSystem manages the state of ping highlights and grids
type PingSystem struct {
	world *engine.World

	enabled bool
}

// NewPingSystem creates a new ping system
func NewPingSystem(world *engine.World) engine.System {
	s := &PingSystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *PingSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *PingSystem) Name() string {
	return "ping"
}

// Priority returns the system's priority
func (s *PingSystem) Priority() int {
	return constant.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPingGridRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(ev event.GameEvent) {
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

	if ev.Type == event.EventPingGridRequest {
		if payload, ok := ev.Payload.(*event.PingGridRequestPayload); ok {
			s.handleGridRequest(payload.Duration)
		}
	}
}

// Update handles time-based logic for ping components
func (s *PingSystem) Update() {
	if !s.enabled {
		return
	}

	entities := s.world.Components.Ping.GetAllEntities()
	dt := s.world.Resources.Time.DeltaTime

	for _, entity := range entities {
		ping, ok := s.world.Components.Ping.GetComponent(entity)
		if !ok {
			continue
		}

		changed := false

		// Update Grid Timer
		if ping.GridActive {
			ping.GridRemaining -= dt
			if ping.GridRemaining <= 0 {
				ping.GridRemaining = 0
				ping.GridActive = false
			}
			changed = true
		}

		// Commit changes back to store
		if changed {
			s.world.Components.Ping.SetComponent(entity, ping)
		}
	}
}

// handleGridRequest activates the grid on the cursor entity
func (s *PingSystem) handleGridRequest(duration time.Duration) {
	// In single player, apply to the main cursor
	entity := s.world.Resources.Cursor.Entity

	ping, ok := s.world.Components.Ping.GetComponent(entity)
	if !ok {
		return
	}

	ping.GridActive = true
	ping.GridRemaining = duration
	s.world.Components.Ping.SetComponent(entity, ping)
}