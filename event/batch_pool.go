package event

import "sync"

// BatchPayload is the canonical pooled payload for batch effect events
// Replaces per-type batch payload structs (DustSpawnBatchRequestPayload, etc.)
type BatchPayload[T any] struct {
	Entries []T
}

// BatchPool provides zero-allocation batch payload recycling for a specific entry type
type BatchPool[T any] struct {
	pool sync.Pool
}

// NewBatchPool creates a pool with pre-allocated entry slice capacity
func NewBatchPool[T any](defaultCap int) *BatchPool[T] {
	return &BatchPool[T]{
		pool: sync.Pool{
			New: func() any {
				return &BatchPayload[T]{
					Entries: make([]T, 0, defaultCap),
				}
			},
		},
	}
}

// Acquire returns a pooled payload with zero-length, retained-capacity slice
func (p *BatchPool[T]) Acquire() *BatchPayload[T] {
	bp := p.pool.Get().(*BatchPayload[T])
	bp.Entries = bp.Entries[:0]
	return bp
}

// Release returns payload to pool
func (p *BatchPool[T]) Release(bp *BatchPayload[T]) {
	if bp == nil {
		return
	}
	bp.Entries = bp.Entries[:0]
	p.pool.Put(bp)
}

// EmitBatch acquires a pooled payload, copies entries, and pushes to queue
// Single generic API replacing per-type emit helpers
func EmitBatch[T any](q *EventQueue, pool *BatchPool[T], eventType EventType, entries []T) {
	if len(entries) == 0 {
		return
	}
	p := pool.Acquire()
	p.Entries = append(p.Entries, entries...)
	q.Push(GameEvent{
		Type:    eventType,
		Payload: p,
	})
}

// --- Pool instances ---

var (
	FlashBatchPool   = NewBatchPool[FlashSpawnEntry](512)
	BlossomBatchPool = NewBatchPool[BlossomSpawnEntry](128)
	DecayBatchPool   = NewBatchPool[DecaySpawnEntry](128)
	DustBatchPool    = NewBatchPool[DustSpawnEntry](2048)
	FadeoutBatchPool = NewBatchPool[FadeoutSpawnEntry](512)
)