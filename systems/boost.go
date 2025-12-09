package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

type BoostSystem struct {
	ctx *engine.GameContext
}

func NewBoostSystem(ctx *engine.GameContext) *BoostSystem {
	return &BoostSystem{ctx: ctx}
}

// Priority returns the system's priority
func (bs *BoostSystem) Priority() int {
	return constants.PriorityBoost
}

// EventTypes returns the event types BoostSystem handles
func (bs *BoostSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventBoostRequest,
	}
}

// HandleEvent processes boost requests
func (bs *BoostSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventBoostRequest {
		bs.activateBoost(event.Timestamp)
	}
}

// Update handles boost timer and shield Sources bitmask management
func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	bs.ctx.State.UpdateBoostTimerAtomic(now)
}

// activateBoost enables boost logic
func (bs *BoostSystem) activateBoost(now time.Time) {
	endTime := now.Add(constants.BoostBaseDuration)

	// Maximize heat to ensure consistent gameplay state (Boost implies Max Heat)
	bs.ctx.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: constants.MaxHeat}, now)

	// CRITICAL: Set end time BEFORE enabling boost to prevent race condition
	bs.ctx.State.SetBoostEndTime(endTime)
	bs.ctx.State.SetBoostColor(1) // Default to blue boost
	bs.ctx.State.SetBoostEnabled(true)
}