// @lixen: #focus{vfx[ping,grid],event[dispatch]}
// @lixen: #interact{init[ping],state[cursor,time],end[ping]}
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// PingSystem manages the state of ping highlights and grids
// It handles timer countdowns and configuration updates based on game state
type PingSystem struct {
	ctx *engine.GameContext
}

// NewPingSystem creates a new ping system
func NewPingSystem(ctx *engine.GameContext) *PingSystem {
	return &PingSystem{
		ctx: ctx,
	}
}

// Priority returns the system's priority
// Should run before rendering to ensure visual state is up to date
func (s *PingSystem) Priority() int {
	// Run with Effects to ensure state is ready for render
	return 300 // PriorityEffects
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventPingGridRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventPingGridRequest {
		if payload, ok := event.Payload.(*events.PingGridRequestPayload); ok {
			s.handleGridRequest(world, payload.Duration)
		}
	}
}

// Update handles time-based logic for ping components
func (s *PingSystem) Update(world *engine.World, dt time.Duration) {
	// Update all entities with a PingComponent
	entities := world.Pings.All()

	for _, entity := range entities {
		ping, ok := world.Pings.Get(entity)
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
		if inputRes, ok := engine.GetResource[*engine.InputResource](world.Resources); ok {
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
			world.Pings.Add(entity, ping)
		}
	}
}

// handleGridRequest activates the grid on the cursor entity
func (s *PingSystem) handleGridRequest(world *engine.World, duration time.Duration) {
	// In single player, apply to the main cursor
	entity := s.ctx.CursorEntity

	ping, ok := world.Pings.Get(entity)
	if !ok {
		return
	}

	ping.GridActive = true
	ping.GridRemaining = duration
	world.Pings.Add(entity, ping)
}