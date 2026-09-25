package event

import (
	"sync"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// --- Death request pool ---

// Can't use generic batch pool without duplicating shared data or wrapper, both inefficient

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
	p.Behavior = component.ParticleNone
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
	p.EffectEvent = EventNone
	p.Behavior = component.ParticleNone
	deathRequestPool.Put(p)
}

// --- Explosion batch pool ---

var explosionBatchRequestPool = sync.Pool{
	New: func() any {
		return &ExplosionBatchRequestPayload{
			Centers: make([]ExplosionCenterEntry, 0, parameter.ExplosionCenterCap),
		}
	},
}

// AcquireExplosionBatchRequest returns a pooled payload with a zero-length retained-capacity slice
func AcquireExplosionBatchRequest() *ExplosionBatchRequestPayload {
	return explosionBatchRequestPool.Get().(*ExplosionBatchRequestPayload)
}

// ReleaseExplosionBatchRequest returns payload to pool
func ReleaseExplosionBatchRequest(p *ExplosionBatchRequestPayload) {
	if p == nil {
		return
	}
	// A recycled crossing ID would seed the next user's knockback from another run.
	*p = ExplosionBatchRequestPayload{Centers: p.Centers[:0]}
	explosionBatchRequestPool.Put(p)
}

// ReleaseDeferredPayload returns a pooled payload after the barrier encoded it.
// Non-pooled payloads need no action.
func ReleaseDeferredPayload(payload any) {
	if p, ok := payload.(*ExplosionBatchRequestPayload); ok {
		ReleaseExplosionBatchRequest(p)
	}
}
