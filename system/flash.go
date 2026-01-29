package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// FlashSystem manages the lifecycle of visual flash effects
type FlashSystem struct {
	world *engine.World

	enabled bool
}

func NewFlashSystem(world *engine.World) engine.System {
	s := &FlashSystem{
		world: world,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *FlashSystem) Init() {
	s.enabled = true
}

// Name returns system's name
func (s *FlashSystem) Name() string {
	return "flash"
}

func (s *FlashSystem) Priority() int {
	return parameter.PriorityFlash
}

func (s *FlashSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFlashRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *FlashSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	if ev.Type == event.EventFlashRequest {
		if payload, ok := ev.Payload.(*event.FlashRequestPayload); ok {
			s.spawnDestructionFlash(payload.X, payload.Y, payload.Char)
		}
	}
}

func (s *FlashSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	entities := s.world.Components.Flash.GetAllEntities()
	for _, entity := range entities {
		flash, ok := s.world.Components.Flash.GetComponent(entity)
		if !ok {
			continue
		}

		flash.Remaining -= dt
		if flash.Remaining <= 0 {
			s.world.DestroyEntity(entity)
		} else {
			s.world.Components.Flash.SetComponent(entity, flash)
		}
	}
}

// spawnDestructionFlash creates a flash effect at the given position
func (s *FlashSystem) spawnDestructionFlash(x, y int, char rune) {
	flash := component.FlashComponent{
		X:         x,
		Y:         y,
		Char:      char,
		Remaining: parameter.DestructionFlashDuration,
		Duration:  parameter.DestructionFlashDuration,
	}

	entity := s.world.CreateEntity()
	s.world.Components.Flash.SetComponent(entity, flash)
}