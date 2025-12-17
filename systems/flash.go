package systems

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// FlashSystem manages the lifecycle of visual flash effects
type FlashSystem struct {
	world *engine.World
	res   engine.CoreResources

	flashStore *engine.Store[components.FlashComponent]
}

func NewFlashSystem(world *engine.World) engine.System {
	return &FlashSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		flashStore: engine.GetStore[components.FlashComponent](world),
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

func (s *FlashSystem) HandleEvent(event events.GameEvent) {
	if event.Type == events.EventFlashRequest {
		if payload, ok := event.Payload.(*events.FlashRequestPayload); ok {
			s.spawnDestructionFlash(payload.X, payload.Y, payload.Char)
		}
	}
}

func (s *FlashSystem) Update() {
	dt := s.res.Time.DeltaTime
	entities := s.flashStore.All()
	for _, entity := range entities {
		flash, ok := s.flashStore.Get(entity)
		if !ok {
			continue
		}

		flash.Remaining -= dt
		if flash.Remaining <= 0 {
			s.world.DestroyEntity(entity)
		} else {
			s.flashStore.Add(entity, flash)
		}
	}
}

// spawnDestructionFlash creates a flash effect at the given position
func (s *FlashSystem) spawnDestructionFlash(x, y int, char rune) {
	flash := components.FlashComponent{
		X:         x,
		Y:         y,
		Char:      char,
		Remaining: constants.DestructionFlashDuration,
		Duration:  constants.DestructionFlashDuration,
	}

	entity := s.world.CreateEntity()
	s.flashStore.Add(entity, flash)
}