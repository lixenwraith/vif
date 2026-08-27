package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// PingSystem manages the state of ping highlights and grids
type PingSystem struct {
	world             *engine.World
	statCursorRejects *atomic.Int64
	statDisabled      *atomic.Int64

	enabled bool
}

// NewPingSystem creates a new ping system
func NewPingSystem(world *engine.World) engine.System {
	s := &PingSystem{
		world: world,
	}
	s.statCursorRejects = world.Resources.Status.Ints.Get("ping.cursor_rejects")
	s.statDisabled = world.Resources.Status.Ints.Get("ping.disabled_rejects")
	s.Init()
	return s
}

// Init resets session state for new game
func (s *PingSystem) Init() {
	s.statCursorRejects.Store(0)
	s.statDisabled.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *PingSystem) Name() string {
	return "ping"
}

// Domain reports player: PingComponent is pure local view (D-13).
func (s *PingSystem) Domain() engine.SystemDomain { return engine.SystemPlayer }

// Requires the cursor the ping component lives on.
func (s *PingSystem) Requires() engine.SystemDependencies {
	return engine.Require("cursor")
}

// Priority returns the system's priority
func (s *PingSystem) Priority() int {
	return parameter.PriorityEffect
}

// EventTypes returns the event types PingSystem handles
func (s *PingSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventPingGridRequest,
		event.EventCursorDespawned,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

// HandleEvent processes ping-related events
func (s *PingSystem) HandleEvent(ev event.GameEvent) {
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
			s.statDisabled.Add(1)
		}
		return
	}

	switch ev.Type {

	case event.EventPingGridRequest:
		p, ok := ev.Payload.(*event.PingGridRequestPayload)
		if !ok {
			return
		}
		if target := s.world.ResolveCursor(p.Entity); target != 0 {
			s.handleGridRequest(target, p.Duration)
		} else {
			s.statCursorRejects.Add(1)
		}
	}
}

// Update handles time-based logic for ping components
func (s *PingSystem) Update() {
	if !s.enabled {
		return
	}

	pings := s.world.Components.Ping
	dt := s.world.Resources.Time.DeltaTime

	for _, entity := range pings.Entities() {
		ping, ok := pings.GetPtr(entity)
		if !ok {
			continue
		}

		// Update Grid Timer
		if ping.GridActive {
			ping.GridRemaining -= dt
			if ping.GridRemaining <= 0 {
				ping.GridRemaining = 0
				ping.GridActive = false
			}
		}
	}
}

// handleGridRequest activates the grid on one cursor
func (s *PingSystem) handleGridRequest(entity core.Entity, duration time.Duration) {
	ping, ok := s.world.Components.Ping.GetPtr(entity)
	if !ok {
		return
	}
	ping.GridActive = true
	ping.GridRemaining = duration
}
