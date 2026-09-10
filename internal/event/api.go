package event

import (
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// EmitDeath requests destruction of one or more entities with an optional effect.
// The variadic arguments do not escape; the pooled payload owns a copy after return.
//
// The record carries the domain of the entities dying rather than the caller's
// ambient domain, which is what lets EmitDeath bypass World.PushEvent and still
// stamp correctly. A shared system claiming cells kills occupants of both domains
// (D-12), so mixed input is split into one batch per domain: a shared death record
// never names a player entity.
func EmitDeath(q *EventQueue, effect EventType, entities ...core.Entity) {
	emitDeath(q, effect, component.ParticleNone, entities...)
}

// EmitParticleDeath requests destruction followed by a particle effect. The
// behavior travels beside the unified particle event discriminator.
func EmitParticleDeath(q *EventQueue, behavior component.ParticleBehavior, entities ...core.Entity) {
	emitDeath(q, EventParticleSpawnOne, behavior, entities...)
}

func emitDeath(q *EventQueue, effect EventType, behavior component.ParticleBehavior, entities ...core.Entity) {
	if len(entities) == 0 {
		return
	}

	domain := entities[0].Domain()
	for _, e := range entities[1:] {
		if e.Domain() != domain {
			// Rare: callers that sweep cells already split by hand.
			pushDeathBatch(q, effect, behavior, core.DomainShared, entities, true)
			pushDeathBatch(q, effect, behavior, core.DomainPlayer, entities, true)
			return
		}
	}
	pushDeathBatch(q, effect, behavior, domain, entities, false)
}

// pushDeathBatch emits one domain-pure batch, selecting members when the caller
// mixed domains. An empty selection returns its payload rather than pushing.
func pushDeathBatch(q *EventQueue, effect EventType, behavior component.ParticleBehavior, domain core.Domain, entities []core.Entity, filter bool) {
	p := AcquireDeathRequest(effect)
	p.Behavior = behavior
	if filter {
		for _, e := range entities {
			if e.Domain() == domain {
				p.Entities = append(p.Entities, e)
			}
		}
	} else {
		p.Entities = append(p.Entities, entities...)
	}

	if len(p.Entities) == 0 {
		ReleaseDeathRequest(p)
		return
	}
	q.Push(GameEvent{
		Type:    EventDeathBatch,
		Payload: p,
		Domain:  domain,
	})
}

// Pattern 1: Individual Kill (e.g., TypingSystem, NuggetSystem)
// event.EmitDeath(s.res.Event.Queue, 0, entity)
//
// Pattern 2: Individual Kill with Flash Effect (e.g., Typing correct char)
// event.EmitDeath(s.res.Event.Queue, event.EventFlashSpawnOneRequest, entity)
//
// Pattern 3: Batch Kill (e.g., Cleaner sweep, particle row)
// 'toDestroy' is prepared []core.Entity slice
// event.EmitDeath(s.res.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy...)
//
// Pattern 4: Particle Effect
// event.EmitParticleDeath(s.res.Event.Queue, component.ParticleBlossom, toDestroy...)
//
// Pattern 5: Silent Batch (e.g., Range delete)
// event.EmitDeath(s.res.Event.Queue, 0, toDestroy...)
