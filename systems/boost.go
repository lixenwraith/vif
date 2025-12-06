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

// Update handles boost timer and shield Sources bitmask management
func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	bs.ctx.State.UpdateBoostTimerAtomic(now)
}