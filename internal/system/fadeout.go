package system

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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
		if batch, ok := ev.Payload.(*event.BatchPayload[event.FadeoutSpawnEntry]); ok {
			for i := range batch.Entries {
				e := &batch.Entries[i]
				s.spawnFadeout(e.X, e.Y, e.Char, e.FgColor, e.BgColor)
			}
			event.FadeoutBatchPool.Release(batch)
		}
	}
}

func (s *FadeoutSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime
	fadeouts := s.world.Components.Fadeout
	var toDestroy []core.Entity

	for _, entity := range fadeouts.Entities() {
		fadeout, ok := fadeouts.GetPtr(entity)
		if !ok {
			continue
		}

		fadeout.Remaining -= dt
		if fadeout.Remaining <= 0 {
			toDestroy = append(toDestroy, entity)
		}
	}

	s.world.DestroyEntitiesBatch(toDestroy)
}

func (s *FadeoutSystem) spawnFadeout(x, y int, char rune, fgColor, bgColor color.RGB) {
	entity := s.world.CreateEntity()
	s.world.Components.Fadeout.SetComponent(entity, component.FadeoutComponent{
		Char:      char,
		FgColor:   fgColor,
		BgColor:   bgColor,
		Remaining: parameter.FadeoutDuration,
		Duration:  parameter.FadeoutDuration,
	})
	s.world.Positions.SetPosition(entity, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(entity,
		component.ProtectionComponent{Mask: component.ProtectFromDecay})
}
