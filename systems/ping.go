package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// PingSystem manages the state of ping highlights and grids
type PingSystem struct {
	world *engine.World
	res   engine.CoreResources

	pingStore *engine.Store[components.PingComponent]
}

// NewPingSystem creates a new ping system
func NewPingSystem(world *engine.World) engine.System {
	return &PingSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		pingStore: engine.GetStore[components.PingComponent](world),
	}
}

// Init
func (s *PingSystem) Init() {}

// Priority returns the system's priority
// Should run before rendering to ensure visual state is up to date
func (s *PingSystem) Priority() int {
	return constants.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventPingGridRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(event events.GameEvent) {
	if event.Type == events.EventPingGridRequest {
		if payload, ok := event.Payload.(*events.PingGridRequestPayload); ok {
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

		// Sync styling with GameMode (Context Awareness)
		targetColor := components.ColorNormal
		if inputRes, ok := engine.GetResource[*engine.InputResource](s.world.Resources); ok {
			if inputRes.GameMode == engine.ResourceModeInsert {
				targetColor = components.ColorNormal // or specific Insert color if defined
			}
		}

		if ping.CrosshairColor != targetColor {
			ping.CrosshairColor = targetColor
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