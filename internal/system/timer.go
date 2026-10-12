package system

import (
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TimerSystem ages every entity's countdowns before any owner reads them, so a
// countdown expires on the tick its owner would have counted it down to zero
type TimerSystem struct {
	world   *engine.World
	expired []core.Entity

	toggle
}

func NewTimerSystem(world *engine.World) engine.System {
	s := &TimerSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *TimerSystem) Init() {
	s.enabled = true
}

func (s *TimerSystem) Name() string {
	return "timer"
}

func (s *TimerSystem) Priority() int {
	return parameter.PriorityTimer
}

func (s *TimerSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *TimerSystem) HandleEvent(ev event.GameEvent) {
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
}

// Update ages every armed slot; a lifetime that runs out leaves through a death
// batch, settled before the next tick's systems run
func (s *TimerSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	timers := s.world.Components.Timer
	s.expired = s.expired[:0]

	for _, entity := range timers.Entities() {
		timer, ok := timers.GetPtr(entity)
		if !ok {
			continue
		}
		for slot := range timer.Slots {
			c := &timer.Slots[slot]
			if c.Remaining <= 0 {
				continue
			}
			if c.Remaining -= dt; c.Remaining <= 0 {
				c.Remaining = 0
				if component.TimerSlot(slot) == component.TimerLifetime {
					s.expired = append(s.expired, entity)
				}
			}
		}
	}

	event.EmitDeath(s.world.Resources.Event.Queue, 0, s.expired...)
}

// armTimer starts one of e's countdowns, attaching its timer on first use
func armTimer(w *engine.World, e core.Entity, slot component.TimerSlot, d time.Duration) {
	if timer, ok := w.Components.Timer.GetPtr(e); ok {
		timer.Arm(slot, d)
		return
	}
	var timer component.TimerComponent
	timer.Arm(slot, d)
	w.Components.Timer.SetComponent(e, timer)
}
