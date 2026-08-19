package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// ShieldSystem owns shield activation and drain; every cursor carries its own shield
type ShieldSystem struct {
	world *engine.World

	statActive    *status.PlayerBool
	statShieldHit *atomic.Int64

	enabled bool
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) engine.System {
	s := &ShieldSystem{world: world}

	reg := world.Resources.Status
	s.statActive = status.NewPlayerBool(reg, parameter.MaxPlayers, "shield.active", "shield.active")
	s.statShieldHit = reg.Ints.Get("shield.shield_hit")

	s.Init()
	return s
}

// Init resets session state for a new game
func (s *ShieldSystem) Init() {
	s.statActive.Reset()
	s.statShieldHit.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *ShieldSystem) Name() string { return "shield" }

// Priority returns the system's priority
func (s *ShieldSystem) Priority() int { return parameter.PriorityShield }

// EventTypes returns the event types ShieldSystem handles
func (s *ShieldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventShieldActivate,
		event.EventShieldDeactivate,
		event.EventShieldDrainRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes shield commands, each naming the cursor it acts on
func (s *ShieldSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
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

	if ev.Type == event.EventCursorDespawned {
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.statActive.Store(p.Slot, false)
		}
		return
	}

	cursor := s.world.TargetCursor(ev.Payload)
	if cursor == 0 {
		return
	}

	switch ev.Type {
	case event.EventShieldActivate:
		s.setActive(cursor, true)

	case event.EventShieldDeactivate:
		s.setActive(cursor, false)

	case event.EventShieldDrainRequest:
		if payload, ok := ev.Payload.(*event.ShieldDrainRequestPayload); ok {
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Entity:     cursor,
				Delta:      payload.Value,
				Percentage: false,
				Type:       component.EnergyDeltaPenalty,
			})

			s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
				// SoundType: audio.SoundShield,
			})

			s.statShieldHit.Add(1)
		}
	}
}

// setActive applies shield state to one cursor and refreshes its ping bounds
func (s *ShieldSystem) setActive(cursor core.Entity, active bool) {
	shield, ok := s.world.Components.Shield.GetPtr(cursor)
	if !ok {
		return
	}

	if active {
		shield.Type = component.ShieldTypePlayer
		cfg := &visual.ShieldConfigs[shield.Type]
		shield.RadiusX = cfg.RadiusX
		shield.RadiusY = cfg.RadiusY
		shield.InvRxSq = cfg.InvRxSq
		shield.InvRySq = cfg.InvRySq
	}
	shield.Active = active
	s.world.UpdateBoundsRadius()

	if slot, ok := s.world.CursorSlot(cursor); ok {
		s.statActive.Store(slot, active)
	}
}

// Update handles passive shield drain for every shielded cursor
func (s *ShieldSystem) Update() {
	if !s.enabled {
		return
	}

	now := s.world.Resources.Time.GameTime

	s.world.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		shieldComp, ok := s.world.Components.Shield.GetPtr(e)
		if !ok || !shieldComp.Active {
			return true
		}

		if now.Sub(shieldComp.LastDrainTime) >= parameter.ShieldPassiveDrainInterval {
			s.world.PushEvent(event.EventEnergyAddRequest, &event.EnergyAddPayload{
				Entity:     e,
				Delta:      parameter.ShieldPassiveEnergyPercentDrain,
				Percentage: true,
				Type:       component.EnergyDeltaPassive,
			})
			shieldComp.LastDrainTime = now
		}
		return true
	})
}
