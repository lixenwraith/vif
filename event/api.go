package event

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// EmitDeathOne performs a zero-allocation death request for a single entity
// Packs the effect and ID into a uint64 to bypass heap allocation
func EmitDeathOne(q *EventQueue, id core.Entity, effect EventType) {
	// Bit-pack: Effect (High 16 bits) | Entity (Low 48 bits)
	// Supports up to 2^48 entities (~281 trillion)
	packed := (uint64(effect) << 48) | (uint64(id) & 0xFFFFFFFFFFFF)
	q.Push(GameEvent{
		Type:    EventDeathOne,
		Payload: packed,
	})
}

// EmitDeathBatch handles batch destruction using the sync.Pool
// Caller provides a slice; helper handles acquisition and copying
func EmitDeathBatch(q *EventQueue, effect EventType, entities []core.Entity) {
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
// event.EmitDeathOne(s.res.Event.Queue, entity, 0)
//
// Pattern 2: Individual Kill with Flash Effect (e.g., Typing correct char)
// event.EmitDeathOne(s.res.Event.Queue, entity, event.EventFlashSpawnOneRequest)
//
// Pattern 3: Batch Kill (e.g., Cleaner sweep, Decay row)
// 'toDestroy' is prepared []core.Entity slice
// event.EmitDeathBatch(s.res.Event.Queue, event.EventFlashSpawnOneRequest, toDestroy)
//
// **Pattern 4: Silent Batch (e.g., Range delete)**
// event.EmitDeathBatch(s.res.Event.Queue, 0, toDestroy)