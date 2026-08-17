package system

import (
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// CursorSystem applies cursor movement to the player entity when the move
// event settles. The router writes the position ahead of the event for input
// latency, so this is a no-op during play; it is the sole movement path during
// replay, where only the event stream exists.
type CursorSystem struct {
	world *engine.World
}

// NewCursorSystem creates the cursor system
func NewCursorSystem(world *engine.World) engine.System {
	s := &CursorSystem{world: world}
	s.Init()
	return s
}

// Init resets per-session state; the system holds none
func (s *CursorSystem) Init() {}

// Name returns system's name
func (s *CursorSystem) Name() string { return "cursor" }

// Priority returns the system's priority
func (s *CursorSystem) Priority() int { return parameter.PriorityCursor }

// Update is empty: cursor placement is entirely event-driven
func (s *CursorSystem) Update() {}

// EventTypes returns the event types CursorSystem handles.
// EventMetaSystemCommandRequest is absent deliberately: a disabled cursor
// system would strand the cursor during replay, so it carries no toggle.
func (s *CursorSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventCursorMoved}
}

// HandleEvent applies the payload position to the player entity
func (s *CursorSystem) HandleEvent(ev event.GameEvent) {
	p, ok := ev.Payload.(*event.CursorMovedPayload)
	if !ok {
		return
	}

	entity := s.world.Resources.Player.Entity
	pos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return
	}
	if pos.X == p.X && pos.Y == p.Y {
		return // live path: the producer already wrote this value
	}

	pos.X, pos.Y = p.X, p.Y
	s.world.Positions.SetPosition(entity, pos)
}
