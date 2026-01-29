package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// HeatSystem owns HeatComponent mutations
type HeatSystem struct {
	world *engine.World

	statCurrent  *atomic.Int64
	statOverheat *atomic.Int64
	statAtMax    *atomic.Bool

	enabled bool
}

func NewHeatSystem(world *engine.World) engine.System {
	s := &HeatSystem{
		world: world,
	}

	s.statCurrent = s.world.Resources.Status.Ints.Get("heat.current")
	s.statOverheat = s.world.Resources.Status.Ints.Get("heat.overheat")
	s.statAtMax = s.world.Resources.Status.Bools.Get("heat.at_max")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *HeatSystem) Init() {
	s.statCurrent.Store(0)
	s.statOverheat.Store(0)
	s.statAtMax.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *HeatSystem) Name() string {
	return "heat"
}

func (s *HeatSystem) Priority() int {
	return parameter.PriorityHeat
}

func (s *HeatSystem) Update() {
	if !s.enabled {
		return
	}

	cursorEntity := s.world.Resources.Cursor.Entity
	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}

	if heatComp.BurstFlashRemaining > 0 {
		dt := s.world.Resources.Time.DeltaTime
		heatComp.BurstFlashRemaining -= dt
		if heatComp.BurstFlashRemaining <= 0 {
			heatComp.BurstFlashRemaining = 0
		}
		s.world.Components.Heat.SetComponent(cursorEntity, heatComp)
	}
}

func (s *HeatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventHeatAddRequest,
		event.EventHeatSetRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *HeatSystem) HandleEvent(ev event.GameEvent) {
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
	case event.EventHeatAddRequest:
		if payload, ok := ev.Payload.(*event.HeatAddRequestPayload); ok {
			s.addHeat(payload.Delta)
		}
	case event.EventHeatSetRequest:
		if payload, ok := ev.Payload.(*event.HeatSetRequestPayload); ok {
			s.setHeat(payload.Value)
		}
	}
}

// addHeat applies delta with clamping and writes back to store
func (s *HeatSystem) addHeat(delta int) {
	cursorEntity := s.world.Resources.Cursor.Entity

	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Reset overheat if heat penalty
	if delta < 0 {
		heatComp.Overheat = 0
		s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
			SoundType: core.SoundMetalHit,
		})
	}

	// Update heat, clamp to bounds, update overheat
	current := heatComp.Current
	newVal := current + delta
	if newVal < 0 {
		newVal = 0
	}
	if newVal > parameter.HeatMax {
		overheat := newVal - parameter.HeatMax
		newVal = parameter.HeatMax
		heatComp.Overheat += overheat
	}
	heatComp.Current = newVal

	// Trigger and reset overheat if at or above max
	if heatComp.Overheat >= parameter.HeatMaxOverheat {
		heatComp.Overheat = 0
		heatComp.BurstFlashRemaining = parameter.HeatBurstFlashDuration
		s.world.PushEvent(event.EventHeatBurstNotification, nil)
	}

	s.world.Components.Heat.SetComponent(cursorEntity, heatComp)

	s.statCurrent.Store(int64(heatComp.Current))
	s.statOverheat.Store(int64(heatComp.Overheat))
	s.statAtMax.Store(newVal >= parameter.HeatMax)
}

// setHeat stores absolute value with clamping and writes back to store
func (s *HeatSystem) setHeat(value int) {
	cursorEntity := s.world.Resources.Cursor.Entity

	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Clamp
	if value < 0 {
		value = 0
	}
	if value > parameter.HeatMax {
		heatComp.Overheat = value - parameter.HeatMax
		value = parameter.HeatMax
	} else {
		heatComp.Overheat = 0
	}

	heatComp.Current = value

	s.statCurrent.Store(int64(value))
	s.statOverheat.Store(int64(heatComp.Overheat))
	s.statAtMax.Store(value >= parameter.HeatMax)

	s.world.Components.Heat.SetComponent(cursorEntity, heatComp)
}