package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
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

func (bs *BoostSystem) Priority() int {
	return constants.PriorityBoost
}

func (bs *BoostSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventBoostActivate,
		events.EventBoostDeactivate,
		events.EventBoostExtend,
	}
}

func (bs *BoostSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	switch event.Type {
	case events.EventBoostActivate:
		if payload, ok := event.Payload.(*events.BoostActivatePayload); ok {
			bs.activate(world, payload.Duration)
		}
	case events.EventBoostDeactivate:
		bs.deactivate(world)
	case events.EventBoostExtend:
		if payload, ok := event.Payload.(*events.BoostExtendPayload); ok {
			bs.extend(world, payload.Duration)
		}
	}
}

// Update handles boost duration decrement using Delta Time
func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	boost, ok := world.Boosts.Get(bs.ctx.CursorEntity)
	if !ok || !boost.Active {
		return
	}

	boost.Remaining -= dt
	if boost.Remaining <= 0 {
		boost.Remaining = 0
		boost.Active = false
	}

	world.Boosts.Add(bs.ctx.CursorEntity, boost)
}

func (bs *BoostSystem) activate(world *engine.World, duration time.Duration) {
	boost, ok := world.Boosts.Get(bs.ctx.CursorEntity)
	if !ok {
		// Should always exist on cursor, but safe fallback
		boost = components.BoostComponent{}
	}

	// If already active, this resets/overwrites duration (or adds? usually activate implies fresh start)
	// Design choice: Activate overwrites. Extend adds.
	boost.Active = true
	boost.Remaining = duration
	boost.TotalDuration = duration // Reset total for UI progress bar if applicable

	world.Boosts.Add(bs.ctx.CursorEntity, boost)

	// Boost implies Max Heat
	// We emit event to keep HeatSystem authoritative
	// Timestamp is not available here in standard helper, would need to pass it or use context
	// For simplicity in this decoupled system, we assume immediate effect
	bs.ctx.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: constants.MaxHeat}, bs.ctx.PausableClock.Now())
}

func (bs *BoostSystem) deactivate(world *engine.World) {
	boost, ok := world.Boosts.Get(bs.ctx.CursorEntity)
	if !ok {
		return
	}
	boost.Active = false
	boost.Remaining = 0
	world.Boosts.Add(bs.ctx.CursorEntity, boost)
}

func (bs *BoostSystem) extend(world *engine.World, duration time.Duration) {
	boost, ok := world.Boosts.Get(bs.ctx.CursorEntity)
	if !ok || !boost.Active {
		return
	}

	boost.Remaining += duration
	// Optionally cap at TotalDuration or allow it to grow?
	// Allowing growth is standard for extensions
	if boost.Remaining > boost.TotalDuration {
		boost.TotalDuration = boost.Remaining
	}

	world.Boosts.Add(bs.ctx.CursorEntity, boost)
}