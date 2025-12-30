// @lixen: #dev{feature[shield(render,system)],feature[spirit(render,system)]}
package event

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// EmitDeathOne performs a zero-allocation death request for a single entity
// Packs the effect and ID into a uint64 to bypass heap allocation
func EmitDeathOne(q *EventQueue, id core.Entity, effect EventType, frame int64) {
	// Bit-pack: Effect (High 16 bits) | Entity (Low 48 bits)
	// Supports up to 2^48 entities (~281 trillion)
	packed := (uint64(effect) << 48) | (uint64(id) & 0xFFFFFFFFFFFF)
	q.Push(GameEvent{
		Type:    EventDeathOne,
		Payload: packed,
		Frame:   frame,
	})
}

// EmitDeathBatch handles batch destruction using the sync.Pool
// Caller provides a slice; helper handles acquisition and copying
func EmitDeathBatch(q *EventQueue, effect EventType, entities []core.Entity, frame int64) {
	if len(entities) == 0 {
		return
	}
	p := AcquireDeathRequest(effect)
	p.Entities = append(p.Entities, entities...)
	q.Push(GameEvent{
		Type:    EventDeathBatch,
		Payload: p,
		Frame:   frame,
	})
}

// Pattern 1: Individual Kill (e.g., TypingSystem, NuggetSystem)
// event.EmitDeathOne(s.res.Events.Queue, entity, 0, s.res.Time.FrameNumber)
//
// Pattern 2: Individual Kill with Flash Effect (e.g., Typing correct char)
// event.EmitDeathOne(s.res.Events.Queue, entity, event.EventFlashRequest, s.res.Time.FrameNumber)
//
// Pattern 3: Batch Kill (e.g., Cleaner sweep, Decay row)
// 'toDestroy' is prepared []core.Entity slice
// event.EmitDeathBatch(s.res.Events.Queue, event.EventFlashRequest, toDestroy, s.res.Time.FrameNumber)
//
// **Pattern 4: Silent Batch (e.g., Range delete)**
// event.EmitDeathBatch(s.res.Events.Queue, 0, toDestroy, s.res.Time.FrameNumber)