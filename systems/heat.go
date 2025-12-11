// @focus: #gameplay { resource, ability } #event { dispatch }
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// HeatSystem owns HeatComponent mutations
// Processes EventHeatAdd and EventHeatSet with clamping to 0-MaxHeat
type HeatSystem struct {
	ctx *engine.GameContext
}

func NewHeatSystem(ctx *engine.GameContext) *HeatSystem {
	return &HeatSystem{ctx: ctx}
}

func (s *HeatSystem) Priority() int {
	return constants.PriorityHeat
}

func (s *HeatSystem) Update(world *engine.World, dt time.Duration) {
	// No tick-based logic; all mutations via events
}

func (s *HeatSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventHeatAdd,
		events.EventHeatSet,
		events.EventManualCleanerTrigger,
	}
}

func (s *HeatSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventHeatAdd:
		if payload, ok := event.Payload.(*events.HeatAddPayload); ok {
			s.addHeat(world, payload.Delta)
		}
	case events.EventHeatSet:
		if payload, ok := event.Payload.(*events.HeatSetPayload); ok {
			s.setHeat(world, payload.Value)
		}
	case events.EventManualCleanerTrigger:
		s.handleManualCleanerTrigger(world, event.Timestamp)
	}
}

// addHeat applies delta with clamping and writes back to store
func (s *HeatSystem) addHeat(world *engine.World, delta int) {
	heatComp, ok := world.Heats.Get(s.ctx.CursorEntity)
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
	world.Heats.Add(s.ctx.CursorEntity, heatComp)
}

// setHeat stores absolute value with clamping and writes back to store
func (s *HeatSystem) setHeat(world *engine.World, value int) {
	heatComp, ok := world.Heats.Get(s.ctx.CursorEntity)
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
	world.Heats.Add(s.ctx.CursorEntity, heatComp)
}

// handleManualCleanerTrigger checks heat cost and triggers cleaner if affordable
func (s *HeatSystem) handleManualCleanerTrigger(world *engine.World, now time.Time) {
	heatComp, ok := world.Heats.Get(s.ctx.CursorEntity)
	if !ok {
		return
	}

	// Check cost (10 heat)
	if heatComp.Current.Load() < 10 {
		return
	}

	// Deduct heat
	s.addHeat(world, -10)

	// Trigger directional cleaner at cursor position
	if pos, ok := world.Positions.Get(s.ctx.CursorEntity); ok {
		s.ctx.PushEvent(events.EventDirectionalCleanerRequest, &events.DirectionalCleanerPayload{
			OriginX: pos.X,
			OriginY: pos.Y,
		}, now)
	}
}