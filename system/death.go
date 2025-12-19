package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// DeathSystem routes death requests through protection checks and effect emission
// Game entities route through here; effect entities bypass via direct DeathComponent
type DeathSystem struct {
	world *engine.World
	res   engine.CoreResources

	deathStore *engine.Store[component.DeathComponent]
	protStore  *engine.Store[component.ProtectionComponent]
	charStore  *engine.Store[component.CharacterComponent]
}

func NewDeathSystem(world *engine.World) engine.System {
	return &DeathSystem{
		world: world,
		res:   engine.GetCoreResources(world),

		deathStore: engine.GetStore[component.DeathComponent](world),
		protStore:  engine.GetStore[component.ProtectionComponent](world),
		charStore:  engine.GetStore[component.CharacterComponent](world),
	}
}

// Init
func (s *DeathSystem) Init() {}

func (s *DeathSystem) Priority() int {
	return constant.PriorityDeath
}

func (s *DeathSystem) EventTypes() []event.EventType {
	return []event.EventType{event.EventRequestDeath}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type != event.EventRequestDeath {
		return
	}
	p, ok := ev.Payload.(*event.DeathRequestPayload)
	if !ok {
		return
	}

	s.processDeathRequest(p)
	event.ReleaseDeathRequest(p)
}

func (s *DeathSystem) processDeathRequest(p *event.DeathRequestPayload) {
	now := s.res.Time.GameTime.UnixNano()

	for _, entity := range p.Entities {
		if entity == 0 {
			continue
		}

		// Protection check
		if prot, ok := s.protStore.Get(entity); ok {
			if !prot.IsExpired(now) && prot.Mask == component.ProtectAll {
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
		s.deathStore.Add(entity, component.DeathComponent{})
	}
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent event.EventType) {
	pos, hasPos := s.world.Positions.Get(entity)
	if !hasPos {
		return
	}

	// Build payload based on effect type
	switch effectEvent {
	case event.EventFlashRequest:
		char, hasChar := s.charStore.Get(entity)
		if !hasChar {
			return
		}
		s.world.PushEvent(event.EventFlashRequest, &event.FlashRequestPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: char.Rune,
		})

		// Future: EventExplosionRequest, EventChainDeathRequest
		// Each case extracts relevant data and builds appropriate payload
	}
}

func (s *DeathSystem) Update() {}