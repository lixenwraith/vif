package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ShieldSystem owns shield activation state and processes drain events
type ShieldSystem struct {
	world *engine.World

	statActive *atomic.Bool

	enabled bool
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) engine.System {
	s := &ShieldSystem{
		world: world,
	}

	s.statActive = s.world.Resource.Status.Bools.Get("shield.active")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *ShieldSystem) Init() {
	s.statActive.Store(false)
	s.enabled = true
}

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
		event.EventGameReset,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resource.Cursor.Entity

	switch ev.Type {
	case event.EventShieldActivate:
		shield, ok := s.world.Component.Shield.GetComponent(cursorEntity)
		if ok {
			// Calculation cache on activation since at init cursor may not be read when game is reset
			rx := vmath.FromFloat(constant.ShieldRadiusX)
			ry := vmath.FromFloat(constant.ShieldRadiusY)
			shield.RadiusX = rx
			shield.RadiusY = ry
			shield.InvRxSq, shield.InvRySq = vmath.EllipseInvRadiiSq(rx, ry)
			shield.Active = true
			s.world.Component.Shield.SetComponent(cursorEntity, shield)
		}
		s.statActive.Store(true)

	case event.EventShieldDeactivate:
		shield, ok := s.world.Component.Shield.GetComponent(cursorEntity)
		if ok {
			shield.Active = false
			s.world.Component.Shield.SetComponent(cursorEntity, shield)
		}
		s.statActive.Store(false)

	case event.EventShieldDrain:
		if payload, ok := ev.Payload.(*event.ShieldDrainPayload); ok {
			s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
				Delta:      payload.Amount,
				Spend:      false, // Subject to boost protection
				Convergent: true,  // Clamp at zero
			})
			s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
				SoundType: core.SoundShield,
			})
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resource.Cursor.Entity

	shield, ok := s.world.Component.Shield.GetComponent(cursorEntity)
	if !ok || !shield.Active {
		return
	}

	now := s.world.Resource.Time.GameTime

	if now.Sub(shield.LastDrainTime) >= constant.ShieldPassiveDrainInterval {
		s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
			Delta:      -constant.ShieldPassiveDrainAmount,
			Spend:      true, // Bypass boost (passive cost)
			Convergent: true, // Clamp at zero
		})
		shield.LastDrainTime = now
		s.world.Component.Shield.SetComponent(cursorEntity, shield)
	}
}