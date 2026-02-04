package event

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

// --- Death pool ---

var deathRequestPool = sync.Pool{
	New: func() any {
		return &DeathRequestPayload{
			Entities: make([]core.Entity, 0, 256),
		}
	},
}

// AcquireDeathRequest returns a pooled payload
func AcquireDeathRequest(effectEvent EventType) *DeathRequestPayload {
	p := deathRequestPool.Get().(*DeathRequestPayload)
	p.Entities = p.Entities[:0]
	p.EffectEvent = effectEvent
	return p
}

// ReleaseDeathRequest returns payload to pool
func ReleaseDeathRequest(p *DeathRequestPayload) {
	if p == nil {
		return
	}
	for i := range p.Entities {
		p.Entities[i] = 0
	}
	p.Entities = p.Entities[:0]
	deathRequestPool.Put(p)
}

// --- Dust pool ---

var dustSpawnBatchPool = sync.Pool{
	New: func() any {
		return &DustSpawnBatchRequestPayload{
			// Pre-allocate capacity for typical explosions to avoid resize allocs
			Entries: make([]DustSpawnEntry, 0, 2048),
		}
	},
}

// AcquireDustSpawnBatch returns a pooled payload with reset slice
func AcquireDustSpawnBatch() *DustSpawnBatchRequestPayload {
	p := dustSpawnBatchPool.Get().(*DustSpawnBatchRequestPayload)
	// Reslice to length 0, keeping capacity
	p.Entries = p.Entries[:0]
	return p
}

// ReleaseDustSpawnBatch returns payload to pool
func ReleaseDustSpawnBatch(p *DustSpawnBatchRequestPayload) {
	if p == nil {
		return
	}
	// Reslice to length 0 to clear references (though DustSpawnEntry contains no pointers, good practice)
	p.Entries = p.Entries[:0]
	dustSpawnBatchPool.Put(p)
}

// --- Fadeout pool ---

var fadeoutSpawnBatchPool = sync.Pool{
	New: func() any {
		return &FadeoutSpawnBatchPayload{
			Entries: make([]FadeoutSpawnEntry, 0, 512),
		}
	},
}

// AcquireFadeoutSpawnBatch returns a pooled payload with reset slice
func AcquireFadeoutSpawnBatch() *FadeoutSpawnBatchPayload {
	p := fadeoutSpawnBatchPool.Get().(*FadeoutSpawnBatchPayload)
	p.Entries = p.Entries[:0]
	return p
}

// ReleaseFadeoutSpawnBatch returns payload to pool
func ReleaseFadeoutSpawnBatch(p *FadeoutSpawnBatchPayload) {
	if p == nil {
		return
	}
	p.Entries = p.Entries[:0]
	fadeoutSpawnBatchPool.Put(p)
}