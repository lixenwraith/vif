package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
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

func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	if bs.ctx.State.UpdateBoostTimerAtomic(now) {
		// Boost expired
	}
}