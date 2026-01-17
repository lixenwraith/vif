package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

type BoostSystem struct {
	world *engine.World

	// Cached metric pointers
	statActive    *atomic.Bool
	statRemaining *atomic.Int64

	enabled bool
}

func NewBoostSystem(world *engine.World) engine.System {
	s := &BoostSystem{
		world: world,
	}

	s.statActive = s.world.Resources.Status.Bools.Get("boost.active")
	s.statRemaining = s.world.Resources.Status.Ints.Get("boost.remaining")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *BoostSystem) Init() {
	s.statActive.Store(false)
	s.statRemaining.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *BoostSystem) Name() string {
	return "boost"
}

func (s *BoostSystem) Priority() int {
	return constant.PriorityBoost
}

func (s *BoostSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventBoostActivate,
		event.EventBoostDeactivate,
		event.EventBoostExtend,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *BoostSystem) HandleEvent(ev event.GameEvent) {
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

	switch ev.Type {
	case event.EventBoostActivate:
		if payload, ok := ev.Payload.(*event.BoostActivatePayload); ok {
			s.activate(payload.Duration)
		}
	case event.EventBoostDeactivate:
		s.deactivate()
	case event.EventBoostExtend:
		if payload, ok := ev.Payload.(*event.BoostExtendPayload); ok {
			s.extend(payload.Duration)
		}
	}
}

// Update handles boost duration decrement using Delta Time
func (s *BoostSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	cursorEntity := s.world.Resources.Cursor.Entity

	boostComp, ok := s.world.Components.Boost.GetComponent(cursorEntity)
	if !ok || !boostComp.Active {
		return
	}

	boostComp.Remaining -= dt
	if boostComp.Remaining <= 0 {
		boostComp.Remaining = 0
		boostComp.Active = false
	}

	s.world.Components.Boost.SetComponent(cursorEntity, boostComp)

	s.statActive.Store(boostComp.Active)
	s.statRemaining.Store(int64(boostComp.Remaining))
}

func (s *BoostSystem) activate(duration time.Duration) {
	cursorEntity := s.world.Resources.Cursor.Entity

	boostComp, ok := s.world.Components.Boost.GetComponent(cursorEntity)
	if !ok {
		boostComp = component.BoostComponent{}
	}

	// If already active, this resets/overwrites duration (or adds? usually activate implies fresh start)
	// Design choice: Activate overwrites. Extend adds.
	boostComp.Active = true
	boostComp.Remaining = duration
	boostComp.TotalDuration = duration // Reset total for UI progress bar if applicable

	s.world.Components.Boost.SetComponent(cursorEntity, boostComp)
}

func (s *BoostSystem) deactivate() {
	cursorEntity := s.world.Resources.Cursor.Entity

	boostComp, ok := s.world.Components.Boost.GetComponent(cursorEntity)
	if !ok {
		return
	}
	boostComp.Active = false
	boostComp.Remaining = 0
	s.world.Components.Boost.SetComponent(cursorEntity, boostComp)
}

func (s *BoostSystem) extend(duration time.Duration) {
	cursorEntity := s.world.Resources.Cursor.Entity

	boostComp, ok := s.world.Components.Boost.GetComponent(cursorEntity)
	if !ok || !boostComp.Active {
		return
	}

	boostComp.Remaining += duration
	// Optionally cap at TotalDuration or allow it to grow?
	// Allowing growth is standard for extensions
	if boostComp.Remaining > boostComp.TotalDuration {
		boostComp.TotalDuration = boostComp.Remaining
	}

	s.world.Components.Boost.SetComponent(cursorEntity, boostComp)
}