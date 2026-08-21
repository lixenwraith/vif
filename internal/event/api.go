package event

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
)

// EmitDeath requests destruction of one or more entities with an optional effect.
// The variadic arguments do not escape; the pooled payload owns a copy after return.
func EmitDeath(q *EventQueue, effect EventType, entities ...core.Entity) {
	if len(entities) == 0 {
		return
	}
	p := AcquireDeathRequest(effect)
	p.Entities = append(p.Entities, entities...)
	q.Push(GameEvent{
		Type:    EventDeathBatch,
		Payload: p,
	})
}

// Pattern 1: Individual Kill (e.g., TypingSystem, NuggetSystem)
// event.EmitDeath(s.res.Event.Queue, 0, entity)
//
// Pattern 2: Individual Kill with Flash Effect (e.g., Typing correct char)
// event.EmitDeath(s.res.Event.Queue, event.EventFlashSpawnOneRequest, entity)
//
// Pattern 3: Batch Kill (e.g., Cleaner sweep, Decay row)
// 'toDestroy' is prepared []core.Entity slice
// event.EmitDeath(s.res.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy...)
//
// **Pattern 4: Silent Batch (e.g., Range delete)**
// event.EmitDeath(s.res.Event.Queue, 0, toDestroy...)
