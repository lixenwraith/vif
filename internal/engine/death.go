package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// The four shapes it is used in:
//
//	w.EmitDeath(0, entity)                                   // individual kill
//	w.EmitDeath(event.EventFlashSpawnOneRequest, entity)     // with an effect
//	w.EmitDeath(event.EventFlashSpawnOneRequest, batch...)   // a swept batch
//	w.EmitDeath(0, batch...)                                 // a silent batch
//
// EmitDeath requests destruction of one or more entities with an optional effect.
// The variadic arguments do not escape; the pooled payload owns a copy after return.
//
// The record carries the domain of the entities dying rather than the producer's
// ambient one, which is D-7's "generic systems resolve the target domain at
// runtime" rather than an exception to it: a shared system claiming cells kills
// occupants of both domains (D-12), so mixed input is split into one batch per
// domain and a shared death record never names a player entity.
//
// It used to build that record and push it straight onto the queue, which is the
// one thing about it that was a boundary hole: not the split, which is the part
// that has to be here, but the push, which skipped World and therefore the trace
// hook, the uninitialised-queue guard and the origin every other producer carries.
// It goes through PushEventFull now.
//
// The origin it names is OriginSystem rather than the ambient one, and that is the
// second half of the exemption. A death is a simulation-internal consequence — a
// glyph consumed, a species defeated, a row decayed — not a producer's own
// artifact, so it is never journaled and never replayed; the events that caused it
// are, and re-deriving the death from them is what makes a replay a reproduction
// rather than a recording. Taking the ambient origin instead would journal a death
// whenever one happened to be raised inside an input-scoped dispatch, and a replay
// would then apply it *and* re-derive it.
func (w *World) EmitDeath(effect event.EventType, entities ...core.Entity) {
	if len(entities) == 0 {
		return
	}
	domain := entities[0].Domain()
	for _, e := range entities[1:] {
		if e.Domain() != domain {
			// Rare: callers that sweep cells already split by hand.
			w.emitDeathBatch(effect, core.DomainShared, entities, true)
			w.emitDeathBatch(effect, core.DomainPlayer, entities, true)
			return
		}
	}
	w.emitDeathBatch(effect, domain, entities, false)
}

// emitDeathBatch emits one domain-pure batch, selecting members when the caller
// mixed domains. An empty selection returns its payload rather than pushing.
func (w *World) emitDeathBatch(effect event.EventType, domain core.Domain, entities []core.Entity, filter bool) {
	p := event.AcquireDeathRequest(effect)
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
		event.ReleaseDeathRequest(p)
		return
	}
	w.PushEventFull(event.EventDeathBatch, p, event.OriginSystem, domain)
}
