package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// HeatSystem owns HeatComponent mutations
type HeatSystem struct {
	world *engine.World
	res   engine.Resources

	heatStore *engine.Store[component.HeatComponent]
}

func NewHeatSystem(world *engine.World) engine.System {
	return &HeatSystem{
		world: world,
		res:   engine.GetResources(world),

		heatStore: engine.GetStore[component.HeatComponent](world),
	}
}

// Init
func (s *HeatSystem) Init() {}

func (s *HeatSystem) Priority() int {
	return constant.PriorityHeat
}

func (s *HeatSystem) Update() {
	// No tick-based logic; all mutations via events
}

func (s *HeatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventHeatAdd,
		event.EventHeatSet,
		event.EventManualCleanerTrigger,
	}
}

func (s *HeatSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventHeatAdd:
		if payload, ok := ev.Payload.(*event.HeatAddPayload); ok {
			s.addHeat(payload.Delta)
		}
	case event.EventHeatSet:
		if payload, ok := ev.Payload.(*event.HeatSetPayload); ok {
			s.setHeat(payload.Value)
		}
	case event.EventManualCleanerTrigger:
		s.handleManualCleanerTrigger()
	}
}

// addHeat applies delta with clamping and writes back to store
func (s *HeatSystem) addHeat(delta int) {
	cursorEntity := s.res.Cursor.Entity

	heatComp, ok := s.heatStore.Get(cursorEntity)
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
	s.heatStore.Set(cursorEntity, heatComp)
}

// setHeat stores absolute value with clamping and writes back to store
func (s *HeatSystem) setHeat(value int) {
	cursorEntity := s.res.Cursor.Entity

	heatComp, ok := s.heatStore.Get(cursorEntity)
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
	s.heatStore.Set(cursorEntity, heatComp)
}

// handleManualCleanerTrigger checks heat cost and triggers cleaner if affordable
func (s *HeatSystem) handleManualCleanerTrigger() {
	cursorEntity := s.res.Cursor.Entity

	heatComp, ok := s.heatStore.Get(cursorEntity)
	if !ok {
		return
	}

	// Check cost (10 heat)
	if heatComp.Current.Load() < 10 {
		return
	}

	// Deduct heat
	s.addHeat(-10)

	// Trigger directional cleaner at cursor position
	if pos, ok := s.world.Positions.Get(cursorEntity); ok {
		s.world.PushEvent(event.EventDirectionalCleanerRequest, &event.DirectionalCleanerPayload{
			OriginX: pos.X,
			OriginY: pos.Y,
		})
	}
}