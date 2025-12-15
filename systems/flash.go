package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// FlashSystem manages the lifecycle of visual flash effects
type FlashSystem struct {
	world *engine.World
	res   engine.CoreResources
}

func NewFlashSystem(world *engine.World) *FlashSystem {
	return &FlashSystem{
		world: world,
		res:   engine.GetCoreResources(world),
	}
}

func (s *FlashSystem) Priority() int {
	return constants.PriorityFlash
}

func (s *FlashSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventFlashRequest,
	}
}

func (s *FlashSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventFlashRequest {
		if payload, ok := event.Payload.(*events.FlashRequestPayload); ok {
			s.spawnDestructionFlash(world, payload.X, payload.Y, payload.Char)
		}
	}
}

func (s *FlashSystem) Update(world *engine.World, dt time.Duration) {
	entities := world.Flashes.All()
	for _, entity := range entities {
		flash, ok := world.Flashes.Get(entity)
		if !ok {
			continue
		}

		flash.Remaining -= dt
		if flash.Remaining <= 0 {
			world.DestroyEntity(entity)
		} else {
			world.Flashes.Add(entity, flash)
		}
	}
}

// spawnDestructionFlash creates a flash effect at the given position
func (s *FlashSystem) spawnDestructionFlash(world *engine.World, x, y int, char rune) {
	flash := components.FlashComponent{
		X:         x,
		Y:         y,
		Char:      char,
		Remaining: constants.DestructionFlashDuration,
		Duration:  constants.DestructionFlashDuration,
	}

	entity := world.CreateEntity()
	world.Flashes.Add(entity, flash)
}