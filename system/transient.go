package system

import (
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
)

// TransientSystem manages short-lived visual effects (grayout, flash)
// Owns TransientResource lifecycle
type TransientSystem struct {
	world *engine.World

	statGrayoutActive *atomic.Bool
	statStrobeActive  *atomic.Bool

	enabled bool
}

func NewTransientSystem(world *engine.World) engine.System {
	s := &TransientSystem{
		world: world,
	}

	s.statGrayoutActive = world.Resources.Status.Bools.Get("effects.grayout_active")
	s.statStrobeActive = world.Resources.Status.Bools.Get("effects.strobe_active")

	s.Init()
	return s
}

func (s *TransientSystem) Init() {
	s.world.Resources.Transient.Reset()
	s.statGrayoutActive.Store(false)
	s.statStrobeActive.Store(false)
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
		event.EventStrobeRequest,
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

	case event.EventStrobeRequest:
		if payload, ok := ev.Payload.(*event.StrobeRequestPayload); ok {
			s.handleStrobeRequest(payload)
		}
	}
}

func (s *TransientSystem) Update() {
	if !s.enabled {
		return
	}

	// // Decay logic: to be wired in if required
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

	strobe := &s.world.Resources.Transient.Strobe
	if !strobe.Active {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	strobe.Remaining -= dt

	if strobe.Remaining <= 0 {
		strobe.Active = false
		s.statStrobeActive.Store(false)
	}
}

func (s *TransientSystem) handleStrobeRequest(req *event.StrobeRequestPayload) {
	current := &s.world.Resources.Transient.Strobe

	duration := time.Duration(req.DurationMs) * time.Millisecond
	if duration == 0 {
		duration = visual.StrobeDefaultDuration
	}

	// Max stacking: compare intensity * remaining seconds
	if current.Active {
		currentWeight := current.Intensity * current.Remaining.Seconds()
		incomingWeight := req.Intensity * duration.Seconds()
		if currentWeight >= incomingWeight {
			return // Keep current
		}
	}

	current.Active = true
	current.Color = req.Color
	current.Intensity = req.Intensity
	current.InitialDuration = duration
	current.Remaining = duration

	s.statStrobeActive.Store(true)
}