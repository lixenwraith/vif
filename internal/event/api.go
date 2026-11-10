package event

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// EmitDeath asks for the entities' destruction, leaving effect where each stood.
// The arguments do not escape: the pooled payload owns a copy. A record carries
// the domain of its entities, not the caller's, and mixed input is split into one
// batch a domain (D-12), so a shared death record never names a player entity.
func EmitDeath(q *EventQueue, effect component.DeathEffect, entities ...core.Entity) {
	emitDeath(q, effect, component.ParticleNone, entities...)
}

// EmitParticleDeath asks for the entities' destruction, each glyph becoming a
// particle of the behavior
func EmitParticleDeath(q *EventQueue, behavior component.ParticleBehavior, entities ...core.Entity) {
	emitDeath(q, component.DeathParticle, behavior, entities...)
}

func emitDeath(q *EventQueue, effect component.DeathEffect, behavior component.ParticleBehavior, entities ...core.Entity) {
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
func pushDeathBatch(q *EventQueue, effect component.DeathEffect, behavior component.ParticleBehavior, domain core.Domain, entities []core.Entity, filter bool) {
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
