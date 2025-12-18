package systems

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// DeathSystem routes death requests through protection checks and effect emission
// Game entities route through here; effect entities bypass via direct DeathComponent
type DeathSystem struct {
	world *engine.World
	res   engine.CoreResources

	deathStore *engine.Store[components.DeathComponent]
	protStore  *engine.Store[components.ProtectionComponent]
	charStore  *engine.Store[components.CharacterComponent]
}

func NewDeathSystem(world *engine.World) engine.System {
	return &DeathSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		deathStore: engine.GetStore[components.DeathComponent](world),
		protStore:  engine.GetStore[components.ProtectionComponent](world),
		charStore:  engine.GetStore[components.CharacterComponent](world),
	}
}

func (s *DeathSystem) Priority() int {
	return constants.PriorityDeath
}

func (s *DeathSystem) EventTypes() []events.EventType {
	return []events.EventType{events.EventRequestDeath}
}

func (s *DeathSystem) HandleEvent(event events.GameEvent) {
	if event.Type != events.EventRequestDeath {
		return
	}
	p, ok := event.Payload.(*events.DeathRequestPayload)
	if !ok {
		return
	}

	s.processDeathRequest(p)
	events.ReleaseDeathRequest(p)
}

func (s *DeathSystem) processDeathRequest(p *events.DeathRequestPayload) {
	now := s.res.Time.GameTime.UnixNano()

	for _, entity := range p.Entities {
		if entity == 0 {
			continue
		}

		// Protection check
		if prot, ok := s.protStore.Get(entity); ok {
			if !prot.IsExpired(now) && prot.Mask == components.ProtectAll {
				continue
			}
		}

		// Skip if already marked
		if s.deathStore.Has(entity) {
			continue
		}

		// Emit effect event if specified
		if p.EffectEvent != 0 {
			s.emitEffect(entity, p.EffectEvent)
		}

		// Tag for CullSystem
		s.deathStore.Add(entity, components.DeathComponent{})
	}
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent events.EventType) {
	pos, hasPos := s.world.Positions.Get(entity)
	if !hasPos {
		return
	}

	// Build payload based on effect type
	switch effectEvent {
	case events.EventFlashRequest:
		char, hasChar := s.charStore.Get(entity)
		if !hasChar {
			return
		}
		s.world.PushEvent(events.EventFlashRequest, &events.FlashRequestPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: char.Rune,
		})

		// Future: EventExplosionRequest, EventChainDeathRequest
		// Each case extracts relevant data and builds appropriate payload
	}
}

func (s *DeathSystem) Update() {}