package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// ShieldSystem owns shield activation state and processes drain events
type ShieldSystem struct {
	world *engine.World
	res   engine.Resources

	shieldStore *engine.Store[component.ShieldComponent]

	statActive *atomic.Bool
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &ShieldSystem{
		world: world,
		res:   res,

		shieldStore: engine.GetStore[component.ShieldComponent](world),

		statActive: res.Status.Bools.Get("shield.active"),
	}
	return s
}

// Init
func (s *ShieldSystem) Init() {}

// Priority returns the system's priority
func (s *ShieldSystem) Priority() int {
	return constant.PriorityShield
}

// EventTypes returns the event types ShieldSystem handles
func (s *ShieldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventShieldActivate,
		event.EventShieldDeactivate,
		event.EventShieldDrain,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(ev event.GameEvent) {
	cursorEntity := s.res.Cursor.Entity

	switch ev.Type {
	case event.EventShieldActivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = true
			s.shieldStore.Add(cursorEntity, shield)
		}
		s.statActive.Store(true)

	case event.EventShieldDeactivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = false
			s.shieldStore.Add(cursorEntity, shield)
		}
		s.statActive.Store(false)

	case event.EventShieldDrain:
		if payload, ok := ev.Payload.(*event.ShieldDrainPayload); ok {
			s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
				Delta: -payload.Amount,
			})
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update() {
	cursorEntity := s.res.Cursor.Entity

	shield, ok := s.shieldStore.Get(cursorEntity)
	if !ok || !shield.Active {
		return
	}

	now := s.res.Time.GameTime

	if now.Sub(shield.LastDrainTime) >= constant.ShieldPassiveDrainInterval {
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
			Delta: -constant.ShieldPassiveDrainAmount,
		})
		shield.LastDrainTime = now
		s.shieldStore.Add(cursorEntity, shield)
	}
}