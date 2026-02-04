package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// FadeoutSystem manages the lifecycle of visual fadeout effects
type FadeoutSystem struct {
	world *engine.World

	enabled bool
}

func NewFadeoutSystem(world *engine.World) engine.System {
	s := &FadeoutSystem{
		world: world,
	}
	s.Init()
	return s
}

func (s *FadeoutSystem) Init() {
	s.enabled = true
}

func (s *FadeoutSystem) Name() string {
	return "fadeout"
}

func (s *FadeoutSystem) Priority() int {
	return parameter.PriorityFadeout
}

func (s *FadeoutSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventFadeoutSpawnOne,
		event.EventFadeoutSpawnBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *FadeoutSystem) HandleEvent(ev event.GameEvent) {
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
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventFadeoutSpawnOne:
		if payload, ok := ev.Payload.(*event.FadeoutSpawnPayload); ok {
			s.spawnFadeout(payload.X, payload.Y, payload.Char, payload.FgColor, payload.BgColor)
		}

	case event.EventFadeoutSpawnBatch:
		if payload, ok := ev.Payload.(*event.FadeoutSpawnBatchPayload); ok {
			for _, entry := range payload.Entries {
				s.spawnFadeout(entry.X, entry.Y, entry.Char, entry.FgColor, entry.BgColor)
			}
			event.ReleaseFadeoutSpawnBatch(payload)
		}
	}
}

func (s *FadeoutSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	entities := s.world.Components.Fadeout.GetAllEntities()

	for _, entity := range entities {
		fadeout, ok := s.world.Components.Fadeout.GetComponent(entity)
		if !ok {
			continue
		}

		fadeout.Remaining -= dt
		if fadeout.Remaining <= 0 {
			s.world.DestroyEntity(entity)
		} else {
			s.world.Components.Fadeout.SetComponent(entity, fadeout)
		}
	}
}

func (s *FadeoutSystem) spawnFadeout(x, y int, char rune, fgColor, bgColor terminal.RGB) {
	entity := s.world.CreateEntity()
	s.world.Components.Fadeout.SetComponent(entity, component.FadeoutComponent{
		X:         x,
		Y:         y,
		Char:      char,
		FgColor:   fgColor,
		BgColor:   bgColor,
		Remaining: parameter.FadeoutDuration,
		Duration:  parameter.FadeoutDuration,
	})
}