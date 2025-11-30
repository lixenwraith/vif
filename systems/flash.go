package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// FlashSystem manages the lifecycle of visual flash effects
type FlashSystem struct {
	ctx *engine.GameContext
}

func NewFlashSystem(ctx *engine.GameContext) *FlashSystem {
	return &FlashSystem{ctx: ctx}
}

func (s *FlashSystem) Priority() int {
	return constants.PriorityFlash
}

func (s *FlashSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	entities := world.Flashes.All()
	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		if now.Sub(flash.StartTime) >= flash.Duration {
			world.DestroyEntity(entity)
		}
	}
}

// SpawnDestructionFlash creates a flash effect at the given position
// Call from any system when destroying an entity with visual feedback
func SpawnDestructionFlash(world *engine.World, x, y int, char rune, now time.Time) {
	flash := components.FlashComponent{
		X:         x,
		Y:         y,
		Char:      char,
		StartTime: now,
		Duration:  constants.DestructionFlashDuration,
	}

	entity := world.CreateEntity()
	world.Flashes.Add(entity, flash)
}