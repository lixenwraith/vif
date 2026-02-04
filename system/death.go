package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// DeathSystem routes death requests through protection checks and effect emission
// Game entities route through here; effect entities bypass via direct DeathComponent
type DeathSystem struct {
	world *engine.World

	statKilled *atomic.Int64

	enabled bool
}

func NewDeathSystem(world *engine.World) engine.System {
	// res := engine.GetResourceStore(world)
	s := &DeathSystem{
		world: world,
	}

	s.statKilled = s.world.Resources.Status.Ints.Get("death.killed")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *DeathSystem) Init() {
	s.statKilled.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *DeathSystem) Name() string {
	return "death"
}

func (s *DeathSystem) Priority() int {
	return parameter.PriorityDeath
}

func (s *DeathSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDeathOne,
		event.EventDeathBatch,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
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

	switch ev.Type {
	case event.EventDeathOne:
		// HOT PATH: Priority check for bit-packed uint64
		if packed, ok := ev.Payload.(uint64); ok {
			// Bit-pack decode, skipping heap allocation, use event.EmitDeathOne
			entity := core.Entity(packed & 0xFFFFFFFFFFFF)
			effect := event.EventType(packed >> 48)
			s.markForDeath(entity, effect)
			return
		}

		// DEV/SAFETY PATH: Fallback for direct core.Entity calls
		if entity, ok := ev.Payload.(core.Entity); ok {
			s.markForDeath(entity, 0)
			return
		}

	case event.EventDeathBatch:
		if p, ok := ev.Payload.(*event.DeathRequestPayload); ok {
			for _, entity := range p.Entities {
				s.markForDeath(entity, p.EffectEvent)
			}
			event.ReleaseDeathRequest(p)
		}
	}
}

// markForDeath performs protection checks, triggers effects, and DESTROYS the entity immediately
func (s *DeathSystem) markForDeath(entity core.Entity, effect event.EventType) {
	if entity == 0 {
		return
	}

	// 1. Protection Check
	if protComp, ok := s.world.Components.Protection.GetComponent(entity); ok {
		if protComp.Mask.Has(component.ProtectFromDeath) || protComp.Mask == component.ProtectAll {
			// If immortal, remove tag to not process again in Update()
			s.world.Components.Death.RemoveEntity(entity)
			return
		}
	}

	// 2. Emit effect event
	if effect != 0 {
		s.emitEffect(entity, effect)
	}

	// 3. Immediate destruction: removes entity from all stores
	s.world.DestroyEntity(entity)

	s.statKilled.Add(1)
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent event.EventType) {
	entityPos, ok := s.world.Positions.GetPosition(entity)
	if !ok {
		return
	}

	// Fadeout handles its own data extraction from WallComponent
	if effectEvent == event.EventFadeoutSpawnOne {
		if wallComp, ok := s.world.Components.Wall.GetComponent(entity); ok {
			s.world.PushEvent(event.EventFadeoutSpawnOne, &event.FadeoutSpawnPayload{
				X:       entityPos.X,
				Y:       entityPos.Y,
				Char:    wallComp.Char,
				FgColor: wallComp.FgColor,
				BgColor: wallComp.BgColor,
			})
		}
		return
	}

	// Extract char: glyph first, sigil fallback
	var char rune
	var level component.GlyphLevel
	if glyphComp, ok := s.world.Components.Glyph.GetComponent(entity); ok {
		char = glyphComp.Rune
		level = glyphComp.Level
	} else if sigilComp, ok := s.world.Components.Sigil.GetComponent(entity); ok {
		char = sigilComp.Rune
	} else {
		return
	}

	switch effectEvent {
	case event.EventFlashRequest:
		s.world.PushEvent(event.EventFlashRequest, &event.FlashRequestPayload{
			X:    entityPos.X,
			Y:    entityPos.Y,
			Char: char,
		})

	case event.EventBlossomSpawnOne:
		s.world.PushEvent(event.EventBlossomSpawnOne, &event.BlossomSpawnPayload{
			X:             entityPos.X,
			Y:             entityPos.Y,
			Char:          char,
			SkipStartCell: true,
		})

	case event.EventDecaySpawnOne:
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:             entityPos.X,
			Y:             entityPos.Y,
			Char:          char,
			SkipStartCell: true,
		})

	case event.EventDustSpawnOneRequest:
		s.world.PushEvent(event.EventDustSpawnOneRequest, &event.DustSpawnOneRequestPayload{
			X:     entityPos.X,
			Y:     entityPos.Y,
			Char:  char,
			Level: level,
		})
	}
}

// Update processes entities tagged with DeathComponent
func (s *DeathSystem) Update() {
	if !s.enabled {
		return
	}

	deathEntities := s.world.Components.Death.GetAllEntities()
	if len(deathEntities) == 0 {
		return
	}

	for _, deathEntity := range deathEntities {
		// Route through markForDeath to ensure protection checks and visual effects are applied
		s.markForDeath(deathEntity, 0)
	}
}