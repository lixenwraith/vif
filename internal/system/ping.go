package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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
	return parameter.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPingGridRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(ev event.GameEvent) {
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

	pings := s.world.Components.Ping
	dt := s.world.Resources.Time.DeltaTime

	for _, entity := range pings.Entities() {
		ping, ok := pings.GetPtr(entity)
		if !ok {
			continue
		}

		// Update Grid Timer
		if ping.GridActive {
			ping.GridRemaining -= dt
			if ping.GridRemaining <= 0 {
				ping.GridRemaining = 0
				ping.GridActive = false
			}
		}
	}
}

// handleGridRequest activates the grid on the cursor entity
func (s *PingSystem) handleGridRequest(duration time.Duration) {
	// In single player, apply to the main cursor
	entity := s.world.Resources.Player.Entity

	ping, ok := s.world.Components.Ping.GetComponent(entity)
	if !ok {
		return
	}

	ping.GridActive = true
	ping.GridRemaining = duration
	s.world.Components.Ping.SetComponent(entity, ping)
}
