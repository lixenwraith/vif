package system

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// PingSystem manages the state of ping highlights and grids
type PingSystem struct {
	world    *engine.World
	res      engine.Resources
	stateRes *engine.GameStateResource

	pingStore *engine.Store[component.PingComponent]
}

// NewPingSystem creates a new ping system
func NewPingSystem(world *engine.World) engine.System {
	return &PingSystem{
		world: world,
		res:   engine.GetResources(world),

		pingStore: engine.GetStore[component.PingComponent](world),
	}
}

// Init
func (s *PingSystem) Init() {}

// Priority returns the system's priority
// Should run before rendering to ensure visual state is up to date
func (s *PingSystem) Priority() int {
	return constant.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPingGridRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventPingGridRequest {
		if payload, ok := ev.Payload.(*event.PingGridRequestPayload); ok {
			s.handleGridRequest(payload.Duration)
		}
	}
}

// Update handles time-based logic for ping components
func (s *PingSystem) Update() {
	entities := s.pingStore.All()
	dt := s.res.Time.DeltaTime

	for _, entity := range entities {
		ping, ok := s.pingStore.Get(entity)
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
			s.pingStore.Add(entity, ping)
		}
	}
}

// handleGridRequest activates the grid on the cursor entity
func (s *PingSystem) handleGridRequest(duration time.Duration) {
	// In single player, apply to the main cursor
	entity := s.res.Cursor.Entity

	ping, ok := s.pingStore.Get(entity)
	if !ok {
		return
	}

	ping.GridActive = true
	ping.GridRemaining = duration
	s.pingStore.Add(entity, ping)
}