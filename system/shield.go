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

	statActive    *atomic.Bool
	statShieldHit *atomic.Int64

	enabled bool
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) engine.System {
	s := &ShieldSystem{
		world: world,
	}

	s.statActive = s.world.Resources.Status.Bools.Get("shield.active")
	s.statShieldHit = s.world.Resources.Status.Ints.Get("shield.shield_hit")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *ShieldSystem) Init() {
	s.statActive.Store(false)
	s.statShieldHit.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *ShieldSystem) Name() string {
	return "shield"
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
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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

	cursorEntity := s.world.Resources.Cursor.Entity

	switch ev.Type {
	case event.EventShieldActivate:
		shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
		if ok {
			// Calculation cache on activation since at init cursor may not be read when game is reset
			rx := vmath.FromFloat(constant.ShieldRadiusX)
			ry := vmath.FromFloat(constant.ShieldRadiusY)
			shield.RadiusX = rx
			shield.RadiusY = ry
			shield.InvRxSq, shield.InvRySq = vmath.EllipseInvRadiiSq(rx, ry)
			shield.Active = true
			s.world.Components.Shield.SetComponent(cursorEntity, shield)
		}
		s.statActive.Store(true)

	case event.EventShieldDeactivate:
		shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
		if ok {
			shield.Active = false
			s.world.Components.Shield.SetComponent(cursorEntity, shield)
		}
		s.statActive.Store(false)

	case event.EventShieldDrain:
		if payload, ok := ev.Payload.(*event.ShieldDrainPayload); ok {
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Delta:      payload.Amount,
				Percentage: false,
				Type:       event.EnergyDeltaPenalty,
			})
			s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
				SoundType: core.SoundShield,
			})
			s.statShieldHit.Add(1)
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Cursor.Entity

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if !ok || !shieldComp.Active {
		return
	}

	now := s.world.Resources.Time.GameTime

	if now.Sub(shieldComp.LastDrainTime) >= constant.ShieldPassiveDrainInterval {
		s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
			Delta:      constant.ShieldPassiveEnergyPercentDrain,
			Percentage: true,
			Type:       event.EnergyDeltaPenalty,
		})
		shieldComp.LastDrainTime = now
		s.world.Components.Shield.SetComponent(cursorEntity, shieldComp)
	}
}