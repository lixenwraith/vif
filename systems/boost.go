package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
)

type BoostSystem struct {
	ctx *engine.GameContext
}

func NewBoostSystem(ctx *engine.GameContext) *BoostSystem {
	return &BoostSystem{ctx: ctx}
}

func (bs *BoostSystem) Priority() int {
	return 5 // Run early, before energy system
}

func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	if bs.ctx.State.UpdateBoostTimerAtomic(now) {
		// Boost expired
	}
}