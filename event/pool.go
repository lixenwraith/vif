package event

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/core"
)

var deathRequestPool = sync.Pool{
	New: func() any {
		return &DeathRequestPayload{
			Entities: make([]core.Entity, 0, 32),
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