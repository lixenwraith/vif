package system

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// HeatSystem owns HeatComponent mutations
type HeatSystem struct {
	engine.SystemBase

	enabled bool
}

func NewHeatSystem(world *engine.World) engine.System {
	s := &HeatSystem{
		SystemBase: engine.NewSystemBase(world),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *HeatSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *HeatSystem) initLocked() {
	s.enabled = true
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
		event.EventGameReset,
		event.EventHeatAdd,
		event.EventHeatSet,
	}
}

func (s *HeatSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventHeatAdd:
		if payload, ok := ev.Payload.(*event.HeatAddPayload); ok {
			s.addHeat(payload.Delta)
		}
	case event.EventHeatSet:
		if payload, ok := ev.Payload.(*event.HeatSetPayload); ok {
			s.setHeat(payload.Value)
		}
	}
}

// addHeat applies delta with clamping and writes back to store
func (s *HeatSystem) addHeat(delta int) {
	cursorEntity := s.Resource.Cursor.Entity

	heatComp, ok := s.Component.Heat.Get(cursorEntity)
	if !ok {
		return
	}

	// CAS loop is unnecessary on a local copy
	current := heatComp.Current.Load()
	newVal := current + int64(delta)

	// Clamp
	if newVal < 0 {
		newVal = 0
	}
	if newVal > int64(constant.MaxHeat) {
		newVal = int64(constant.MaxHeat)
	}

	heatComp.Current.Store(newVal)

	// CRITICAL: Write the modified component copy back to the store
	s.Component.Heat.Set(cursorEntity, heatComp)
}

// setHeat stores absolute value with clamping and writes back to store
func (s *HeatSystem) setHeat(value int) {
	cursorEntity := s.Resource.Cursor.Entity

	heatComp, ok := s.Component.Heat.Get(cursorEntity)
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

	heatComp.Current.Store(int64(value))

	// CRITICAL: Write the modified component copy back to the store
	s.Component.Heat.Set(cursorEntity, heatComp)
}