package event

import (
	"sync"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
)

// --- Death pool ---

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

// --- Wall batch pool ---

var wallBatchRequestPool = sync.Pool{
	New: func() any {
		return &WallBatchSpawnRequestPayload{
			Cells: make([]component.WallCellDef, 0, 512),
		}
	},
}

// AcquireWallBatchRequest returns a pooled payload with zero-length retained-capacity slice
func AcquireWallBatchRequest() *WallBatchSpawnRequestPayload {
	p := wallBatchRequestPool.Get().(*WallBatchSpawnRequestPayload)
	p.Cells = p.Cells[:0]
	p.X = 0
	p.Y = 0
	p.BlockMask = 0
	p.BoxStyle = 0
	p.CollisionMode = 0
	p.Composite = false
	return p
}

// ReleaseWallBatchRequest returns payload to pool
func ReleaseWallBatchRequest(p *WallBatchSpawnRequestPayload) {
	if p == nil {
		return
	}
	p.Cells = p.Cells[:0]
	wallBatchRequestPool.Put(p)
}