package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
)

// BoostSystem owns BoostComponent; every cursor carries its own boost
type BoostSystem struct {
	world *engine.World

	statActive    *status.PlayerBool
	statRemaining *status.PlayerInt
	statTruncated *atomic.Int64
	rejects       rejectionTelemetry

	enabled bool
}

// NewBoostSystem creates a new boost system
func NewBoostSystem(world *engine.World) engine.System {
	s := &BoostSystem{world: world}

	reg := world.Resources.Status
	s.statActive = status.NewPlayerBool(reg, parameter.MaxPlayers, "boost.active", "boost.active")
	s.statRemaining = status.NewPlayerInt(reg, parameter.MaxPlayers, "boost.remaining", "boost.remaining")
	s.statTruncated = reg.Ints.Get("boost.truncated")
	s.rejects = newRejectionTelemetry(reg, "boost")

	s.Init()
	return s
}

// Init resets session state for a new game
func (s *BoostSystem) Init() {
	s.statActive.Reset()
	s.statRemaining.Reset()
	s.statTruncated.Store(0)
	s.rejects.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *BoostSystem) Name() string { return "boost" }

// Priority returns the system's priority
func (s *BoostSystem) Priority() int { return parameter.PriorityBoost }

// EventTypes returns the event types BoostSystem handles
func (s *BoostSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBoostActivate,
		event.EventBoostDeactivate,
		event.EventBoostExtend,
		event.EventBoostReward,
		event.EventSpeciesKilled,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes boost commands, each naming the cursor it acts on
func (s *BoostSystem) HandleEvent(ev event.GameEvent) {
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
		if ev.Type != event.EventMetaSystemCommandRequest {
			s.rejects.disabled.Add(1)
		}
		return
	}

	if ev.Type == event.EventCursorDespawned {
		if p, ok := ev.Payload.(*event.CursorDespawnedPayload); ok {
			s.statActive.Store(p.Slot, false)
			s.statRemaining.Store(p.Slot, 0)
		}
		return
	}

	switch ev.Type {
	case event.EventBoostActivate:
		if payload, ok := ev.Payload.(*event.BoostActivatePayload); ok {
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.activate(cursor, payload.Duration)
		}
	case event.EventBoostDeactivate:
		if payload, ok := ev.Payload.(*event.BoostDeactivatePayload); ok {
			if cursor := s.world.ResolveOwnedCursor(payload.Entity); cursor != 0 {
				s.deactivate(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}
	case event.EventBoostExtend:
		if payload, ok := ev.Payload.(*event.BoostExtendPayload); ok {
			cursor := s.world.ResolveOwnedCursor(payload.Entity)
			if cursor == 0 {
				s.rejects.cursor.Add(1)
				return
			}
			s.extend(cursor, payload.Duration)
		}

	case event.EventBoostReward:
		if payload, ok := ev.Payload.(*event.BoostRewardPayload); ok {
			if cursor := s.world.ResolveOwnedCursor(payload.Entity); cursor != 0 {
				s.reward(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

	case event.EventSpeciesKilled:
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			// Player-owned tower lifecycle deaths use the shared species event so
			// FSMs can observe them, but are not failed reward requests.
			if payload.Species == component.SpeciesTower && payload.KillerEntity == 0 {
				return
			}
			if cursor := s.world.ResolveOwnedCursor(payload.KillerEntity); cursor != 0 {
				s.reward(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}
	}
}

// reward applies the activation/extension contract at handling time, so no producer
// decides between them from a read that a later dispatch invalidates.
func (s *BoostSystem) reward(cursor core.Entity) {
	boostComp, ok := s.world.Components.Boost.GetPtr(cursor)
	if !ok {
		return
	}
	if boostComp.Active {
		boostComp.Remaining += parameter.BoostExtensionDuration
		if boostComp.Remaining > boostComp.TotalDuration {
			boostComp.TotalDuration = boostComp.Remaining
		}
	} else {
		boostComp.Active = true
		boostComp.Remaining = parameter.BoostBaseDuration
		boostComp.TotalDuration = parameter.BoostBaseDuration
	}
	s.publish(cursor, boostComp)
}

// rewardKill applies the same activation/extension contract as correct typing
// to the cursor credited with the fatal combat hit.
func (s *BoostSystem) rewardKill(cursor core.Entity) {
	boostComp, ok := s.world.Components.Boost.GetPtr(cursor)
	if !ok {
		return
	}
	if boostComp.Active {
		s.extend(cursor, parameter.BoostExtensionDuration)
		return
	}
	s.activate(cursor, parameter.BoostBaseDuration)
}

// Update decrements each cursor's boost by delta time
func (s *BoostSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	s.world.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		// D-2: a remote cursor's boost timer runs on its owner, not here
		if !s.world.SimulatesLocally(e) {
			return true
		}

		boostComp, ok := s.world.Components.Boost.GetPtr(e)
		if !ok || !boostComp.Active {
			return true
		}

		boostComp.Remaining -= dt
		if boostComp.Remaining <= 0 {
			boostComp.Remaining = 0
			boostComp.Active = false
		}
		s.publish(e, boostComp)
		return true
	})
}

// activate starts a fresh boost on one cursor; it is a hard set, rewards go through reward
func (s *BoostSystem) activate(cursor core.Entity, duration time.Duration) {
	boostComp, ok := s.world.Components.Boost.GetPtr(cursor)
	if !ok {
		return // CursorSystem attaches BoostComponent, so a miss is not a cursor
	}

	// An explicit activate shortening a live boost is a producer bug, not a reward
	if boostComp.Active && boostComp.Remaining > duration {
		s.statTruncated.Add(1)
	}

	boostComp.Active = true
	boostComp.Remaining = duration
	boostComp.TotalDuration = duration // Reset total for the UI progress bar
	s.publish(cursor, boostComp)
}

// deactivate clears boost on one cursor
func (s *BoostSystem) deactivate(cursor core.Entity) {
	boostComp, ok := s.world.Components.Boost.GetPtr(cursor)
	if !ok {
		return
	}
	boostComp.Active = false
	boostComp.Remaining = 0
	s.publish(cursor, boostComp)
}

// extend adds duration to an active boost, growing the total for the progress bar
func (s *BoostSystem) extend(cursor core.Entity, duration time.Duration) {
	boostComp, ok := s.world.Components.Boost.GetPtr(cursor)
	if !ok || !boostComp.Active {
		return
	}

	boostComp.Remaining += duration
	if boostComp.Remaining > boostComp.TotalDuration {
		boostComp.TotalDuration = boostComp.Remaining
	}
	s.publish(cursor, boostComp)
}

// publish mirrors one cursor's boost into its roster slot
func (s *BoostSystem) publish(cursor core.Entity, boostComp *component.BoostComponent) {
	slot, ok := s.world.CursorSlot(cursor)
	if !ok {
		return
	}
	s.statActive.Store(slot, boostComp.Active)
	s.statRemaining.Store(slot, int64(boostComp.Remaining))
}
