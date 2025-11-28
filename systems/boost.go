package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
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

// Update handles status bar timer update and shield component lifecycle anchored on cursor
func (bs *BoostSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Update timer state
	bs.ctx.State.UpdateBoostTimerAtomic(now)

	// Sync Shield component with Boost state
	boostEnabled := bs.ctx.State.GetBoostEnabled()
	cursorEntity := bs.ctx.CursorEntity

	hasShield := world.Shields.Has(cursorEntity)

	if boostEnabled {
		if !hasShield {
			// Create shield component
			shield := components.ShieldComponent{
				Active:     true,
				RadiusX:    constants.ShieldRadiusX,
				RadiusY:    constants.ShieldRadiusY,
				Color:      render.RgbShieldBase,
				MaxOpacity: constants.ShieldMaxOpacity,
			}
			world.Shields.Add(cursorEntity, shield)
		}
	} else {
		if hasShield {
			// Remove shield when boost is disabled
			world.Shields.Remove(cursorEntity)
		}
	}
}