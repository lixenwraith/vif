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

// Priority returns the system's priority
// Should run before rendering to ensure visual state is up to date
func (s *PingSystem) Priority() int {
	return constant.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventPingGridRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
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

	entities := s.world.Component.Ping.All()
	dt := s.world.Resource.Time.DeltaTime

	for _, entity := range entities {
		ping, ok := s.world.Component.Ping.Get(entity)
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
			s.world.Component.Ping.Set(entity, ping)
		}
	}
}

// handleGridRequest activates the grid on the cursor entity
func (s *PingSystem) handleGridRequest(duration time.Duration) {
	// In single player, apply to the main cursor
	entity := s.world.Resource.Cursor.Entity

	ping, ok := s.world.Component.Ping.Get(entity)
	if !ok {
		return
	}

	ping.GridActive = true
	ping.GridRemaining = duration
	s.world.Component.Ping.Set(entity, ping)
}