package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// HeatSystem owns HeatComponent mutations
type HeatSystem struct {
	world *engine.World

	statCurrent *atomic.Int64
	statAtMax   *atomic.Bool

	enabled bool
}

func NewHeatSystem(world *engine.World) engine.System {
	s := &HeatSystem{
		world: world,
	}

	s.statCurrent = s.world.Resources.Status.Ints.Get("heat.current")
	s.statAtMax = s.world.Resources.Status.Bools.Get("heat.at_max")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *HeatSystem) Init() {
	s.statCurrent.Store(0)
	s.statAtMax.Store(false)
	s.enabled = true
}

// Name returns system's name
func (s *HeatSystem) Name() string {
	return "heat"
}

func (s *HeatSystem) Priority() int {
	return constant.PriorityHeat
}

func (s *HeatSystem) Update() {
	if !s.enabled {
		return
	}
	// No tick-based logic; all mutations via events
}

func (s *HeatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventHeatAdd,
		event.EventHeatSet,
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
	case event.EventHeatAdd:
		if payload, ok := ev.Payload.(*event.HeatAddPayload); ok {
			s.addHeat(payload.Delta)
			if payload.Delta < 0 {
				s.world.PushEvent(event.EventSoundRequest, &event.SoundRequestPayload{
					SoundType: core.SoundMetalHit,
				})
			}
		}
	case event.EventHeatSet:
		if payload, ok := ev.Payload.(*event.HeatSetPayload); ok {
			s.setHeat(payload.Value)
		}
	}
}

// TODO: refactor this and setHead more
// addHeat applies delta with clamping and writes back to store
func (s *HeatSystem) addHeat(delta int) {
	cursorEntity := s.world.Resources.Cursor.Entity

	heatComp, ok := s.world.Components.Heat.GetComponent(cursorEntity)
	if !ok {
		return
	}

	// Update heat and clamp to bounds
	current := heatComp.Current
	newVal := current + delta

	s.setHeat(newVal)
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
	if value > constant.MaxHeat {
		value = constant.MaxHeat
	}

	heatComp.Current = value

	s.statCurrent.Store(int64(value))
	s.statAtMax.Store(value >= constant.MaxHeat)

	// Write the modified component copy back to the store
	s.world.Components.Heat.SetComponent(cursorEntity, heatComp)
}