package system

import (
	"slices"
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

// Update ages every armed slot; an expired lifetime leaves through a death batch,
// settled as the tick closes. Collection is by state, not by the pass that expired
// it: an expired slot whose batch never settled (a disabled DeathSystem, a batch
// a full queue overwrote) is ended by the next pass.
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
			if c := &timer.Slots[slot]; c.Remaining > 0 {
				c.Remaining = max(c.Remaining-dt, 0)
			}
		}
		if !timer.Expired(component.TimerLifetime) {
			continue
		}
		// Death refuses a protected entity; its lifetime ends without it, as the
		// Death tag's did
		if prot, ok := s.world.Components.Protection.GetPtr(entity); ok && prot.Mask&component.ProtectFromDeath != 0 {
			timer.Stop(component.TimerLifetime)
			continue
		}
		s.expired = append(s.expired, entity)
	}

	// Store order may differ between instances after a correction (D-8); the
	// batch must not carry that difference into destruction order
	slices.Sort(s.expired)
	event.EmitDeath(s.world.Resources.Event.Queue, 0, s.expired...)
}

// timerRunning reports one of e's countdowns still running; an entity without a
// timer runs none
func timerRunning(w *engine.World, e core.Entity, slot component.TimerSlot) bool {
	timer, ok := w.Components.Timer.GetPtr(e)
	return ok && timer.Running(slot)
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
