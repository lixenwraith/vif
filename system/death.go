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
	res   engine.Resources

	deathStore *engine.Store[component.DeathComponent]
	protStore  *engine.Store[component.ProtectionComponent]
	charStore  *engine.Store[component.CharacterComponent]
}

func NewDeathSystem(world *engine.World) engine.System {
	return &DeathSystem{
		world: world,
		res:   engine.GetResources(world),

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
	return []event.EventType{event.EventDeathOne, event.EventDeathBatch}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventDeathOne:
		if packed, ok := ev.Payload.(uint64); ok {
			// Bit-pack decode, skipping heap allocation, use event.EmitDeathOne
			id := core.Entity(packed & 0xFFFFFFFFFFFF)
			effect := event.EventType(packed >> 48)
			s.markForDeath(id, effect)
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

// markForDeath performs protection checks and tags entity
func (s *DeathSystem) markForDeath(entity core.Entity, effect event.EventType) {
	if entity == 0 {
		return
	}

	// Protection check
	if prot, ok := s.protStore.Get(entity); ok {
		if !prot.IsExpired(s.res.Time.GameTime.UnixNano()) && prot.Mask == component.ProtectAll {
			return
		}
	}

	// Skip if already marked
	if s.deathStore.Has(entity) {
		return
	}

	// Emit death vfx event if specified
	if effect != 0 {
		s.emitEffect(entity, effect)
	}

	// Tag for CullSystem
	s.deathStore.Add(entity, component.DeathComponent{})
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