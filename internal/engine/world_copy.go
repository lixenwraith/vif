package engine

import (
	"maps"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
)

// WorldCopy is a world's entities detached from it: every store and the occupied
// grid cells in their own order, the masks and both domains' allocators. Systems
// visit stores and cells in that order, so a world restored from it continues
// exactly as the one it was read from, which a capture's re-insertion does not.
type WorldCopy struct {
	components         componentCopies
	positions          storeCopy[component.PositionComponent]
	gridW, gridH       int
	cells              []occupiedCell
	mask               map[core.Entity]uint64
	next               [core.DomainCount]uint64
	created, destroyed [core.DomainCount]int64

	config     ConfigResource
	time       TimeResource
	game       *GameState
	player     PlayerResource
	targets    [component.MaxTargetGroups]TargetGroupState
	transient  TransientResource
	view       ViewResource
	generators []StreamState
	session    uint64
	predicting bool
	shared     bool
}

// occupiedCell is one grid cell holding an entity, so a copy is sized by what the
// map holds rather than by the map.
type occupiedCell struct {
	index int
	cell  Cell
}

// CopyOut reads the world's entities. Caller MUST hold updateMutex.
func (w *World) CopyOut() *WorldCopy {
	p := w.Positions
	c := &WorldCopy{
		components: w.copyComponentsOut(),
		positions: storeCopy[component.PositionComponent]{
			dense: append([]component.PositionComponent(nil), p.dense...), entities: append([]core.Entity(nil), p.entities...)},
		gridW: p.grid.Width,
		gridH: p.grid.Height,
		mask:  maps.Clone(w.componentMask),
		next:  w.nextEntityID,
	}
	for i := range p.grid.Cells {
		if p.grid.Cells[i].Count > 0 {
			c.cells = append(c.cells, occupiedCell{index: i, cell: p.grid.Cells[i]})
		}
	}
	for d := range c.created {
		c.created[d], c.destroyed[d] = w.createdCount[d].Load(), w.destroyedCount[d].Load()
	}

	r := w.Resources
	c.config, c.time, c.player = *r.Config, *r.Time, *r.Player
	c.game = &GameState{}
	c.game.copyFrom(r.Game.State)
	r.Target.mu.RLock()
	c.targets = r.Target.groups
	r.Target.mu.RUnlock()
	c.transient, c.view = *r.Transient, *r.View
	for d := range core.DomainCount {
		c.generators = append(c.generators, r.Rand.SaveStreams(core.Domain(d))...)
	}
	c.session, c.predicting, c.shared = r.Rand.Session(), w.predicting.Load(), w.sessionShared.Load()
	return c
}

// CopyIn replaces the world's entities with a copy. Caller MUST hold updateMutex.
func (w *World) CopyIn(c *WorldCopy) {
	w.copyComponentsIn(c.components)
	p := w.Positions
	clear(p.index)
	p.dense = append(p.dense[:0], c.positions.dense...)
	p.entities = append(p.entities[:0], c.positions.entities...)
	for i, e := range p.entities {
		p.index[e] = int32(i)
	}
	p.grid.Resize(c.gridW, c.gridH)
	for _, oc := range c.cells {
		p.grid.Cells[oc.index] = oc.cell
	}
	w.componentMask = maps.Clone(c.mask)
	w.nextEntityID = c.next
	for d := range c.created {
		w.createdCount[d].Store(c.created[d])
		w.destroyedCount[d].Store(c.destroyed[d])
	}

	r := w.Resources
	*r.Config, *r.Time = c.config, c.time
	reg := r.Player.status
	*r.Player = c.player
	r.Player.status = reg
	r.Player.publishLocalSlot()
	r.Game.State.copyFrom(c.game)
	r.Target.mu.Lock()
	r.Target.groups = c.targets
	r.Target.mu.Unlock()
	*r.Transient, *r.View = c.transient, c.view
	r.Rand.SetSession(c.session)
	w.predicting.Store(c.predicting)
	if c.shared {
		w.MarkSessionShared()
	}
}

// LoadStreams resumes every generator the copy names, in both domains, reporting
// any this world never issued.
func (c *WorldCopy) LoadStreams(r *RandResource) []string {
	var unknown []string
	for d := range core.DomainCount {
		var states []StreamState
		for _, st := range c.generators {
			if st.Domain == core.Domain(d) {
				states = append(states, st)
			}
		}
		unknown = append(unknown, r.LoadStreams(core.Domain(d), states)...)
	}
	return unknown
}
