package systems

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// HeatSystem owns HeatComponent mutations
type HeatSystem struct {
	world *engine.World
	res   engine.CoreResources

	heatStore *engine.Store[components.HeatComponent]
}

func NewHeatSystem(world *engine.World) engine.System {
	return &HeatSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		heatStore: engine.GetStore[components.HeatComponent](world),
	}
}

// Init
func (s *HeatSystem) Init() {}

func (s *HeatSystem) Priority() int {
	return constants.PriorityHeat
}

func (s *HeatSystem) Update() {
	// No tick-based logic; all mutations via events
}

func (s *HeatSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventHeatAdd,
		events.EventHeatSet,
		events.EventManualCleanerTrigger,
	}
}

func (s *HeatSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventHeatAdd:
		if payload, ok := event.Payload.(*events.HeatAddPayload); ok {
			s.addHeat(payload.Delta)
		}
	case events.EventHeatSet:
		if payload, ok := event.Payload.(*events.HeatSetPayload); ok {
			s.setHeat(payload.Value)
		}
	case events.EventManualCleanerTrigger:
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
	if newVal > int64(constants.MaxHeat) {
		newVal = int64(constants.MaxHeat)
	}

	heatComp.Current.Store(newVal)

	// CRITICAL: Write the modified component copy back to the store
	s.heatStore.Add(cursorEntity, heatComp)
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
	if value > constants.MaxHeat {
		value = constants.MaxHeat
	}

	heatComp.Current.Store(int64(value))

	// CRITICAL: Write the modified component copy back to the store
	s.heatStore.Add(cursorEntity, heatComp)
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
		s.world.PushEvent(events.EventDirectionalCleanerRequest, &events.DirectionalCleanerPayload{
			OriginX: pos.X,
			OriginY: pos.Y,
		})
	}
}