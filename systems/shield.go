package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// ShieldSystem owns shield activation state and processes drain events
type ShieldSystem struct {
	world *engine.World
	res   engine.CoreResources

	shieldStore *engine.Store[components.ShieldComponent]
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) *ShieldSystem {
	return &ShieldSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		shieldStore: engine.GetStore[components.ShieldComponent](world),
	}
}

// Priority returns the system's priority
func (s *ShieldSystem) Priority() int {
	return constants.PriorityShield
}

// EventTypes returns the event types ShieldSystem handles
func (s *ShieldSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventShieldActivate,
		events.EventShieldDeactivate,
		events.EventShieldDrain,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	cursorEntity := s.res.Cursor.Entity

	switch event.Type {
	case events.EventShieldActivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = true
			s.shieldStore.Add(cursorEntity, shield)
		}

	case events.EventShieldDeactivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = false
			s.shieldStore.Add(cursorEntity, shield)
		}

	case events.EventShieldDrain:
		if payload, ok := event.Payload.(*events.ShieldDrainPayload); ok {
			world.PushEvent(events.EventEnergyAdd, &events.EnergyAddPayload{
				Delta: -payload.Amount,
			})
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update(world *engine.World, dt time.Duration) {
	cursorEntity := s.res.Cursor.Entity

	shield, ok := s.shieldStore.Get(cursorEntity)
	if !ok || !shield.Active {
		return
	}

	now := s.res.Time.GameTime

	if now.Sub(shield.LastDrainTime) >= constants.ShieldPassiveDrainInterval {
		world.PushEvent(events.EventEnergyAdd, &events.EnergyAddPayload{
			Delta: -constants.ShieldPassiveDrainAmount,
		})
		shield.LastDrainTime = now
		s.shieldStore.Add(cursorEntity, shield)
	}
}