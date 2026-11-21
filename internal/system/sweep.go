package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// Footprint states of one map cell in footprint.grid
const (
	footNone uint8 = iota
	footCovered
	footHeld // covered, and a member already stands on it
)

// footprint is the cells a body claims, as a list and a map-sized grid, and the
// members standing on them, one a cell. A member keeps a cell the body still covers,
// so the spatial grid changes only along a moving outline and a missile homing on a
// member does not see it jump; claimed lists the cells a member came to this stand.
type footprint struct {
	cells        []vmath.Point
	grid         []uint8
	gridW, gridH int
	spare, idle  []int // member indexes without a cell this stand: still positioned, or not
	claimed      []vmath.Point
}

// reset clears the previous footprint, or resizes the grid to a new map
func (f *footprint) reset(width, height int) {
	if width != f.gridW || height != f.gridH {
		f.gridW, f.gridH = width, height
		n := max(0, width*height)
		if cap(f.grid) < n {
			f.grid = make([]uint8, n)
		} else {
			f.grid = f.grid[:n]
			clear(f.grid)
		}
	} else {
		for _, c := range f.cells {
			f.grid[c.Y*f.gridW+c.X] = footNone
		}
	}
	f.cells = f.cells[:0]
}

// add claims one cell on the map
func (f *footprint) add(x, y int) {
	if c := &f.grid[y*f.gridW+x]; *c == footNone {
		*c = footCovered
		f.cells = append(f.cells, vmath.Point{X: x, Y: y})
	}
}

func (f *footprint) covers(x, y int) bool {
	return x >= 0 && x < f.gridW && y >= 0 && y < f.gridH && f.grid[y*f.gridW+x] != footNone
}

// stand puts one of e's members on each cell of the footprint, its offset from the
// header's cell ox, oy, clearing the walls under them: positioned spares move before
// idle ones join, so fewer members enter and leave the grid
func (f *footprint) stand(w *engine.World, e core.Entity, ox, oy int) {
	f.claimed = f.claimed[:0]
	header, ok := w.Components.Header.GetPtr(e)
	if !ok {
		return
	}
	f.spare, f.idle = f.spare[:0], f.idle[:0]
	for i := range header.MemberEntries {
		member := &header.MemberEntries[i]
		if member.Entity == 0 {
			continue
		}
		pos, ok := w.Positions.GetPosition(member.Entity)
		if !ok {
			f.idle = append(f.idle, i)
			continue
		}
		if f.covers(pos.X, pos.Y) {
			if c := &f.grid[pos.Y*f.gridW+pos.X]; *c == footCovered {
				*c = footHeld
				member.OffsetX, member.OffsetY = pos.X-ox, pos.Y-oy
				continue
			}
		}
		f.spare = append(f.spare, i)
	}
	positioned := len(f.spare)
	f.spare = append(f.spare, f.idle...)
	next := 0
	for _, cell := range f.cells {
		if w.Positions.Blocks(cell, 0) {
			w.PushEvent(event.EventWallDespawnRequest, &event.WallDespawnRequestPayload{
				X: cell.X, Y: cell.Y, Width: 1, Height: 1,
			})
		}
		c := &f.grid[cell.Y*f.gridW+cell.X]
		if *c == footHeld {
			continue
		}
		*c = footHeld
		for next < len(f.spare) && !w.Components.Member.HasEntity(header.MemberEntries[f.spare[next]].Entity) {
			next++ // destroyed outside its system; composite reaps the entry
		}
		var member *component.MemberEntry
		if next < len(f.spare) {
			member = &header.MemberEntries[f.spare[next]]
			next++
		} else {
			entity := w.CreateEntity(core.DomainShared)
			w.Components.Member.SetComponent(entity, component.MemberComponent{HeaderEntity: e})
			w.Components.Protection.SetComponent(entity, component.ProtectionComponent{
				Mask: component.ProtectFromParticle | component.ProtectFromSpecies,
			})
			header.MemberEntries = append(header.MemberEntries, component.MemberEntry{Entity: entity})
			member = &header.MemberEntries[len(header.MemberEntries)-1]
		}
		member.OffsetX, member.OffsetY = cell.X-ox, cell.Y-oy
		w.Positions.SetPosition(member.Entity, component.PositionComponent{X: cell.X, Y: cell.Y})
		f.claimed = append(f.claimed, cell)
	}
	// Retain spare identities to avoid reallocating ECS entities as the body contracts.
	for _, i := range f.spare[next:max(next, positioned)] {
		w.Positions.RemoveEntity(header.MemberEntries[i].Entity)
	}
}

// touches reports contact as composites have it: a shield wherever the footprint
// enters its reach, a bare cursor only on a covered cell
func (f *footprint) touches(w *engine.World, cursor core.Entity, x, y int) bool {
	shield, ok := w.Components.Shield.GetPtr(cursor)
	if !ok || !shield.Active || !(shield.Radius > 0) {
		return f.covers(x, y)
	}
	rx, ry := w.ShieldReach(shield)
	for cy := max(0, y-ry); cy <= min(f.gridH-1, y+ry); cy++ {
		for cx := max(0, x-rx); cx <= min(f.gridW-1, x+rx); cx++ {
			if f.covers(cx, cy) && shieldCovers(w, shield, x, y, cx, cy) {
				return true
			}
		}
	}
	return false
}

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
	n := copy(s.buf[:], w.Positions.EntitiesAt(position.Point{X: x, Y: y}, position.ScopeAll))
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

// emit routes each domain's victims through the death pipeline separately
func (s *cellSweep) emit(w *engine.World, effect component.DeathEffect) {
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

// countSpeciesProtected records a rejected victim in its entity domain.
func countSpeciesProtected(e core.Entity, shared, player *atomic.Int64) {
	if e.Domain() == core.DomainPlayer {
		if player != nil {
			player.Add(1)
		}
		return
	}
	if shared != nil {
		shared.Add(1)
	}
}

// breakGoldUnder destroys the gold members a composite's living members cover. A
// shared glyph is a gold member, and every shared species breaks the sequence it
// runs into whether or not its footprint clears anything else; the breach the
// composite then reports ends the sequence.
func breakGoldUnder(w *engine.World, sweep *cellSweep, header core.Entity) {
	h, ok := w.Components.Header.GetPtr(header)
	if !ok {
		return
	}
	sweep.reset()
	for _, m := range h.MemberEntries {
		if m.Entity == 0 {
			continue
		}
		if pos, ok := w.Positions.GetPosition(m.Entity); ok {
			sweep.collect(w, pos.X, pos.Y, func(e core.Entity) bool { return goldMember(w, e) })
		}
	}
	sweep.emit(w, component.DeathFlash)
}

// goldMember reports a shared glyph, which only a gold sequence holds
func goldMember(w *engine.World, e core.Entity) bool {
	return e.Domain() == core.DomainShared && w.Components.Glyph.HasEntity(e)
}

// speciesClearable admits everything a spawn footprint may destroy; cursors,
// walls, and species-protected entities survive.
func speciesClearable(w *engine.World, e core.Entity, protectedShared, protectedPlayer *atomic.Int64) bool {
	if w.Components.Cursor.HasEntity(e) || w.Components.Wall.HasEntity(e) {
		return false
	}
	if prot, ok := w.Components.Protection.GetComponent(e); ok && prot.Mask&component.ProtectFromSpecies != 0 {
		countSpeciesProtected(e, protectedShared, protectedPlayer)
		return false
	}
	return true
}
