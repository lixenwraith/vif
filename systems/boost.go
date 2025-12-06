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

	// boostEnabled := bs.ctx.State.GetBoostEnabled()
	// cursorEntity := bs.ctx.CursorEntity
	//
	// shield, hasShield := world.Shields.Get(cursorEntity)
	//
	// if boostEnabled {
	// 	if !hasShield {
	// 		// Create shield with no color override (derive from GameState)
	// 		shield = components.ShieldComponent{
	// 			Sources:       constants.ShieldSourceBoost,
	// 			RadiusX:       constants.ShieldRadiusX,
	// 			RadiusY:       constants.ShieldRadiusY,
	// 			OverrideColor: components.ColorNone,
	// 			MaxOpacity:    constants.ShieldMaxOpacity,
	// 			LastDrainTime: now,
	// 		}
	// 		world.Shields.Add(cursorEntity, shield)
	// 	} else if shield.Sources&constants.ShieldSourceBoost == 0 {
	// 		// Add SourceBoost to existing shield
	// 		shield.Sources |= constants.ShieldSourceBoost
	// 		world.Shields.Add(cursorEntity, shield)
	// 	}
	// } else {
	// 	if hasShield && shield.Sources&constants.ShieldSourceBoost != 0 {
	// 		// Clear SourceBoost flag
	// 		shield.Sources &^= constants.ShieldSourceBoost
	// 		if shield.Sources == 0 {
	// 			// No sources remain - remove component
	// 			world.Shields.Remove(cursorEntity)
	// 		} else {
	// 			world.Shields.Add(cursorEntity, shield)
	// 		}
	// 	}
	// }
}