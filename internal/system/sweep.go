package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// cellSweep collects the occupants a shared system clears from geometry it claims.
// Enumeration covers both domains (D-12); victims leave as one batch per domain so a
// shared record never names a player entity.
type cellSweep struct {
	shared []core.Entity
	player []core.Entity
	buf    [parameter.MaxEntitiesPerCell]core.Entity
}

// reset drops the previous sweep, retaining capacity
func (s *cellSweep) reset() {
	s.shared = s.shared[:0]
	s.player = s.player[:0]
}

// collect appends the occupants of one cell that clearable admits
func (s *cellSweep) collect(w *engine.World, x, y int, clearable func(core.Entity) bool) {
	n := w.Positions.GetAllEntitiesAtInto(x, y, s.buf[:])
	for i := range n {
		e := s.buf[i]
		if e == 0 || !clearable(e) {
			continue
		}
		if e.Domain() == core.DomainPlayer {
			s.player = append(s.player, e)
			continue
		}
		s.shared = append(s.shared, e)
	}
}

// count returns the total collected victims
func (s *cellSweep) count() int { return len(s.shared) + len(s.player) }

// emit routes each domain's victims through the death pipeline separately
func (s *cellSweep) emit(w *engine.World, effect event.EventType) {
	if len(s.shared) > 0 {
		event.EmitDeath(w.Resources.Event.Queue, effect, s.shared...)
	}
	if len(s.player) > 0 {
		// EmitDeath takes the domain from the entities, so the batch stamps itself.
		event.EmitDeath(w.Resources.Event.Queue, effect, s.player...)
	}
}

// destroy removes both batches directly, bypassing the death pipeline
func (s *cellSweep) destroy(w *engine.World) {
	if len(s.shared) > 0 {
		w.DestroyEntitiesBatch(s.shared)
	}
	if len(s.player) > 0 {
		w.DestroyEntitiesBatch(s.player)
	}
}

// speciesClearable admits everything a spawn footprint may destroy: cursors, walls
// and species-protected entities survive.
func speciesClearable(w *engine.World, e core.Entity, protected *atomic.Int64) bool {
	if w.Components.Cursor.HasEntity(e) || w.Components.Wall.HasEntity(e) {
		return false
	}
	if prot, ok := w.Components.Protection.GetComponent(e); ok && prot.Mask&component.ProtectFromSpecies != 0 {
		if protected != nil {
			protected.Add(1)
		}
		return false
	}
	return true
}
