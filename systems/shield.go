package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// ShieldSystem owns shield activation state and processes drain events
type ShieldSystem struct {
	ctx *engine.GameContext
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(ctx *engine.GameContext) *ShieldSystem {
	return &ShieldSystem{ctx: ctx}
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
	switch event.Type {
	case events.EventShieldActivate:
		shield, ok := world.Shields.Get(s.ctx.CursorEntity)
		if ok {
			shield.Active = true
			world.Shields.Add(s.ctx.CursorEntity, shield)
		}

	case events.EventShieldDeactivate:
		shield, ok := world.Shields.Get(s.ctx.CursorEntity)
		if ok {
			shield.Active = false
			world.Shields.Add(s.ctx.CursorEntity, shield)
		}

	case events.EventShieldDrain:
		if payload, ok := event.Payload.(*events.ShieldDrainPayload); ok {
			s.ctx.State.AddEnergy(-payload.Amount)
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update(world *engine.World, dt time.Duration) {
	shield, ok := world.Shields.Get(s.ctx.CursorEntity)
	if !ok || !shield.Active {
		return
	}

	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Check passive drain interval
	if now.Sub(shield.LastDrainTime) >= constants.ShieldPassiveDrainInterval {
		s.ctx.State.AddEnergy(-constants.ShieldPassiveDrainAmount)
		shield.LastDrainTime = now
		world.Shields.Add(s.ctx.CursorEntity, shield)
	}
}