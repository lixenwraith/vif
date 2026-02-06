package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/terminal"
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
	return parameter.PriorityShield
}

// EventTypes returns the event types ShieldSystem handles
func (s *ShieldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventShieldActivate,
		event.EventShieldDeactivate,
		event.EventShieldDrainRequest,
		event.EventEnergyCrossedZeroNotification,
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

	cursorEntity := s.world.Resources.Player.Entity

	switch ev.Type {
	case event.EventShieldActivate:
		shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
		if ok {
			// Apply pre-calculated physics and visual defaults
			cfg := visual.PlayerShieldConfig
			shield.RadiusX = cfg.RadiusX
			shield.RadiusY = cfg.RadiusY
			shield.InvRxSq = cfg.InvRxSq
			shield.InvRySq = cfg.InvRySq
			shield.MaxOpacity = cfg.MaxOpacity

			// Apply default glow settings (Period handled dynamically by update loop)
			shield.GlowColor = cfg.GlowColor
			shield.GlowIntensity = cfg.GlowIntensity

			// If color isn't initialized, use default positive config
			if shield.Color == (terminal.RGB{}) {
				shield.Color = cfg.Color
				shield.Palette256 = cfg.Palette256
			}

			shield.Active = true

			s.world.Components.Shield.SetComponent(cursorEntity, shield)
			s.world.UpdateBoundsRadius()
		}
		s.statActive.Store(true)

	case event.EventShieldDeactivate:
		shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
		if ok {
			shield.Active = false
			s.world.Components.Shield.SetComponent(cursorEntity, shield)
			s.world.UpdateBoundsRadius()
		}
		s.statActive.Store(false)

	case event.EventShieldDrainRequest:
		if payload, ok := ev.Payload.(*event.ShieldDrainRequestPayload); ok {
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Delta:      payload.Value,
				Percentage: false,
				Type:       event.EnergyDeltaPenalty,
			})
			s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
				SoundType: core.SoundShield,
			})
			s.statShieldHit.Add(1)
		}

	case event.EventEnergyCrossedZeroNotification:
		energyComp, ok := s.world.Components.Energy.GetComponent(cursorEntity)
		if ok {
			s.setShieldPolarityColor(energyComp.Current >= 0)
		}
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Player.Entity

	shieldComp, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if !ok || !shieldComp.Active {
		return
	}

	now := s.world.Resources.Time.GameTime

	// 1. Toggle rotating boost indicator visual glow based state
	if boost, ok := s.world.Components.Boost.GetComponent(cursorEntity); ok {
		if boost.Active && shieldComp.GlowPeriod == 0 {
			shieldComp.GlowPeriod = parameter.ShieldBoostRotationDuration
			s.world.Components.Shield.SetComponent(cursorEntity, shieldComp)
		} else if !boost.Active && shieldComp.GlowPeriod != 0 {
			shieldComp.GlowPeriod = 0
			s.world.Components.Shield.SetComponent(cursorEntity, shieldComp)
		}
	}

	// 2. Handle Passive Drain
	if now.Sub(shieldComp.LastDrainTime) >= parameter.ShieldPassiveDrainInterval {
		s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
			Delta:      parameter.ShieldPassiveEnergyPercentDrain,
			Percentage: true,
			Type:       event.EnergyDeltaPenalty,
		})
		shieldComp.LastDrainTime = now
		s.world.Components.Shield.SetComponent(cursorEntity, shieldComp)
	}
}

// setShieldPolarityColor updates the shield component color based on energy polarity
func (s *ShieldSystem) setShieldPolarityColor(isPositive bool) {
	cursorEntity := s.world.Resources.Player.Entity
	shield, ok := s.world.Components.Shield.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if isPositive {
		shield.Color = visual.RgbCleanerBasePositive
		shield.Palette256 = visual.Shield256Positive
	} else {
		shield.Color = visual.RgbCleanerBaseNegative
		shield.Palette256 = visual.Shield256Negative
	}

	s.world.Components.Shield.SetComponent(cursorEntity, shield)
}