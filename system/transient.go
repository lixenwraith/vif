package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// TransientSystem manages short-lived visual effects (grayout, flash)
// Owns TransientResource lifecycle
type TransientSystem struct {
	world *engine.World

	statGrayoutActive *atomic.Bool

	enabled bool
}

func NewTransientSystem(world *engine.World) engine.System {
	s := &TransientSystem{
		world: world,
	}

	s.statGrayoutActive = world.Resources.Status.Bools.Get("effects.grayout_active")

	s.Init()
	return s
}

func (s *TransientSystem) Init() {
	s.world.Resources.Transient.Reset()
	s.statGrayoutActive.Store(false)
	s.enabled = true
}

func (s *TransientSystem) Name() string {
	return "transient_effects"
}

func (s *TransientSystem) Priority() int {
	return parameter.PriorityEffect
}

func (s *TransientSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGrayoutStart,
		event.EventGrayoutEnd,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *TransientSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventGrayoutStart:
		s.world.Resources.Transient.Grayout = engine.GrayoutState{
			Active:    true,
			Intensity: 1.0,
		}
		s.statGrayoutActive.Store(true)

	case event.EventGrayoutEnd:
		s.world.Resources.Transient.Grayout.Active = false
		s.statGrayoutActive.Store(false)
	}
}

func (s *TransientSystem) Update() {
	if !s.enabled {
		return
	}

	// // Decay logic:
	// grayout := &s.world.Resources.Transient.Grayout
	// if grayout.Active && grayout.Intensity > 0 {
	// 	dt := s.world.Resources.Time.DeltaTime.Seconds()
	// 	decay := dt / visual.GrayoutDuration.Seconds()
	// 	grayout.Intensity -= decay
	// 	if grayout.Intensity <= 0 {
	// 		grayout.Intensity = 0
	// 		grayout.Active = false
	// 		s.statGrayoutActive.Store(false)
	// 	}
	// }
}