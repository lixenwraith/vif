package system

import (
	"sync/atomic"

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

	statKilled *atomic.Int64
}

func NewDeathSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &DeathSystem{
		world: world,
		res:   res,

		deathStore: engine.GetStore[component.DeathComponent](world),
		protStore:  engine.GetStore[component.ProtectionComponent](world),
		charStore:  engine.GetStore[component.CharacterComponent](world),

		statKilled: res.Status.Ints.Get("death.killed"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *DeathSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *DeathSystem) initLocked() {
	s.statKilled.Store(0)
}

func (s *DeathSystem) Priority() int {
	return constant.PriorityDeath
}

func (s *DeathSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDeathOne,
		event.EventDeathBatch,
		event.EventGameReset,
	}
}

func (s *DeathSystem) HandleEvent(ev event.GameEvent) {
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

	case event.EventGameReset:
		s.Init()
	}
}

// markForDeath performs protection checks, triggers effects, and DESTROYS the entity immediately
func (s *DeathSystem) markForDeath(entity core.Entity, effect event.EventType) {
	if entity == 0 {
		return
	}

	// 1. Protection Check
	if prot, ok := s.protStore.Get(entity); ok {
		if !prot.IsExpired(s.res.Time.GameTime.UnixNano()) &&
			(prot.Mask.Has(component.ProtectFromDeath) || prot.Mask == component.ProtectAll) {
			// If immortal, remove tag to not process again in Update()
			s.deathStore.Remove(entity)
			return
		}
	}

	// 2. Routing/Cleanup Hooks
	// Pre-destruction hook for informing other systems
	s.routeCleanup(entity)

	// 3. Visual Effects
	if effect != 0 {
		s.emitEffect(entity, effect)
	}

	// 4. Immediate Destruction
	// Removes entity from ALL stores, making it invisible to next Render snapshot
	s.world.DestroyEntity(entity)

	s.statKilled.Add(1)
}

// routeCleanup handles informing other systems before the entity is purged
func (s *DeathSystem) routeCleanup(entity core.Entity) {
	// TODO: Future Implementation, it's already done with emitEffect, maybe expand and remove this branch, need batch processing
	// Example: If entity has MemberComponent, emit EventMemberDied to CompositeSystem
}

func (s *DeathSystem) emitEffect(entity core.Entity, effectEvent event.EventType) {
	pos, hasPos := s.world.Positions.Get(entity)
	if !hasPos {
		return
	}

	char, hasChar := s.charStore.Get(entity)
	if !hasChar {
		return
	}

	switch effectEvent {
	case event.EventFlashRequest:
		s.world.PushEvent(event.EventFlashRequest, &event.FlashRequestPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: char.Rune,
		})

	case event.EventBlossomSpawnOne:
		s.world.PushEvent(event.EventBlossomSpawnOne, &event.BlossomSpawnPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: char.Rune,
		})

	case event.EventDecaySpawnOne:
		s.world.PushEvent(event.EventDecaySpawnOne, &event.DecaySpawnPayload{
			X:    pos.X,
			Y:    pos.Y,
			Char: char.Rune,
		})

		// Future: EventExplosionRequest, EventChainDeathRequest
		// Each case extracts relevant data and builds appropriate payload
	}
}

// Update processes entities tagged with DeathComponent by systems not using the event path.
// This preserves the "deferred" cleanup logic for OOB or timer-based destruction.
func (s *DeathSystem) Update() {
	entities := s.deathStore.All()
	if len(entities) == 0 {
		return
	}

	for _, entity := range entities {
		// Route through markForDeath to ensure protection checks and visual effects are applied
		s.markForDeath(entity, 0)
	}
}