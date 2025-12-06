package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
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
func (s *ShieldSystem) EventTypes() []engine.EventType {
	return []engine.EventType{
		engine.EventShieldActivate,
		engine.EventShieldDeactivate,
		engine.EventShieldDrain,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(world *engine.World, event engine.GameEvent) {
	switch event.Type {
	case engine.EventShieldActivate:
		s.ctx.State.SetShieldActive(true)

	case engine.EventShieldDeactivate:
		s.ctx.State.SetShieldActive(false)

	case engine.EventShieldDrain:
		if payload, ok := event.Payload.(*engine.ShieldDrainPayload); ok {
			s.ctx.State.AddEnergy(-payload.Amount)
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update(world *engine.World, dt time.Duration) {
	if !s.ctx.State.GetShieldActive() {
		return
	}

	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	shield, ok := world.Shields.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}

	// Check passive drain interval
	if now.Sub(shield.LastDrainTime) >= constants.ShieldPassiveDrainInterval {
		s.ctx.State.AddEnergy(-constants.ShieldPassiveDrainAmount)
		shield.LastDrainTime = now
		world.Shields.Add(s.ctx.CursorEntity, shield)
	}
}