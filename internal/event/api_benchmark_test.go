package event

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/core"
)

func popBenchmarkEvent(q *EventQueue) GameEvent {
	head := q.head.Load()
	idx := head & uint64(len(q.events)-1)
	ev := q.events[idx]
	q.events[idx] = GameEvent{}
	q.published[idx].Store(false)
	q.head.Store(head + 1)
	return ev
}

// pushDeath is what World.EmitDeath does once it has split by domain: the pooled
// payload and the push. The split itself moved to engine, where the push can carry
// a producer origin like every other one; what stays under test here is the
// property that put this in its own file — a death costs no allocation.
func pushDeath(q *EventQueue, effect EventType, entities ...core.Entity) {
	p := AcquireDeathRequest(effect)
	p.Entities = append(p.Entities, entities...)
	if len(p.Entities) == 0 {
		ReleaseDeathRequest(p)
		return
	}
	q.Push(GameEvent{Type: EventDeathBatch, Payload: p, Domain: entities[0].Domain()})
}

func BenchmarkEmitDeathSingle(b *testing.B) {
	q := NewEventQueue()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pushDeath(q, EventFlashSpawnOneRequest, 1)
		ev := popBenchmarkEvent(q)
		ReleaseDeathRequest(ev.Payload.(*DeathRequestPayload))
	}
}

func BenchmarkEmitDeath16(b *testing.B) {
	q := NewEventQueue()
	entities := make([]core.Entity, 16)
	for i := range entities {
		entities[i] = core.Entity(i + 1)
	}
	p := AcquireDeathRequest(EventNone)
	ReleaseDeathRequest(p)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pushDeath(q, EventFlashSpawnOneRequest, entities...)
		ev := popBenchmarkEvent(q)
		ReleaseDeathRequest(ev.Payload.(*DeathRequestPayload))
	}
}

func TestEmitDeathUsesUnifiedPayload(t *testing.T) {
	q := NewEventQueue()
	entities := []core.Entity{7, 9}
	pushDeath(q, EventFlashSpawnOneRequest, entities...)
	entities[0] = 11

	ev := popBenchmarkEvent(q)
	if ev.Type != EventDeathBatch {
		t.Fatalf("event type = %v, want EventDeathBatch", ev.Type)
	}
	p, ok := ev.Payload.(*DeathRequestPayload)
	if !ok {
		t.Fatalf("payload type = %T, want *DeathRequestPayload", ev.Payload)
	}
	defer ReleaseDeathRequest(p)
	if p.EffectEvent != EventFlashSpawnOneRequest {
		t.Fatalf("effect = %v, want EventFlashSpawnOneRequest", p.EffectEvent)
	}
	if len(p.Entities) != 2 || p.Entities[0] != 7 || p.Entities[1] != 9 {
		t.Fatalf("entities = %v, want [7 9]", p.Entities)
	}

	pushed := q.Pushed()
	pushDeath(q, EventNone)
	if q.Pushed() != pushed {
		t.Fatal("empty death request was queued")
	}
}

func TestEmitDeathSingleDoesNotAllocate(t *testing.T) {
	q := NewEventQueue()
	p := AcquireDeathRequest(EventNone)
	ReleaseDeathRequest(p)

	allocs := testing.AllocsPerRun(1000, func() {
		pushDeath(q, EventFlashSpawnOneRequest, 1)
		ev := popBenchmarkEvent(q)
		ReleaseDeathRequest(ev.Payload.(*DeathRequestPayload))
	})
	if allocs != 0 {
		t.Fatalf("a pooled death push allocated %.2f times; want 0", allocs)
	}
}
