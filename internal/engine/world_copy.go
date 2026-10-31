package engine

import (
	"maps"
	"slices"

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
	gridD              int
	cells              []occupiedCell
	occupants          []core.Entity // every occupied cell's entities, cell after cell
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
	latched    SessionState
	shared     bool
}

// occupiedCell is one grid cell holding an entity, its entities in the copy's
// occupants, so a copy is sized by what the map holds rather than by the map or a cell.
type occupiedCell struct {
	index         int32
	count, shared uint8
}

// CopyOut reads the world's entities. reuse, when not nil, is a copy nobody reads any
// longer, whose storage this one takes over. Caller MUST hold updateMutex.
func (w *World) CopyOut(reuse *WorldCopy) *WorldCopy {
	p := w.Positions
	if reuse == nil {
		reuse = &WorldCopy{}
	}
	c := &WorldCopy{
		components: w.copyComponentsOut(&reuse.components),
		positions: storeCopy[component.PositionComponent]{
			dense: append(reuse.positions.dense[:0], p.dense...), entities: append(reuse.positions.entities[:0], p.entities...)},
		gridW: p.Index.Width,
		gridH: p.Index.Height,
		gridD: p.Index.Depth,
		mask:  reuse.mask,
		next:  w.nextEntityID,
	}
	if c.mask == nil {
		c.mask = maps.Clone(w.componentMask)
	} else {
		clear(c.mask)
		maps.Copy(c.mask, w.componentMask)
	}
	// Every held entity has a position, which bounds both without a pass to count them.
	c.cells, c.occupants = sized(reuse.cells, 0, len(p.entities)), sized(reuse.occupants, 0, len(p.entities))
	for _, i := range p.Index.Occupied() {
		cell := &p.Index.Cells[i]
		c.cells = append(c.cells, occupiedCell{index: i, count: cell.Count, shared: cell.SharedCount})
		c.occupants = append(c.occupants, cell.Entities[:cell.Count]...)
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
	c.session, c.latched, c.shared = r.Rand.Session(), w.Session(), w.sessionShared.Load()
	return c
}

// CopyIn replaces the world's entities with a copy. Caller MUST hold updateMutex.
func (w *World) CopyIn(c *WorldCopy) {
	w.copyComponentsIn(c.components)
	p := w.Positions
	p.slots.clear()
	p.dense = append(p.dense[:0], c.positions.dense...)
	p.entities = append(p.entities[:0], c.positions.entities...)
	for i, e := range p.entities {
		p.slots.set(e, int32(i))
	}
	p.Index.Resize(c.gridW, c.gridH, c.gridD)
	at := 0
	for _, oc := range c.cells {
		p.Index.Load(oc.index, oc.count, oc.shared, c.occupants[at:at+int(oc.count)])
		at += int(oc.count)
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
	w.AdoptSession(c.latched)
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

// PlayerSide is what this instance's own player holds in the world's resources: the
// camera, the action rate and gesture tracking, the timed effects, the player
// domain's streams and the cells its input has in flight. A journal's opening
// carries it beside the player world.
type PlayerSide struct {
	CameraX    int               `json:"camera_x"`
	CameraY    int               `json:"camera_y"`
	Pointer    bool              `json:"pointer"`
	APM        APMState          `json:"apm"`
	Explosions []ExplosionCenter `json:"explosions,omitempty"`
	View       ViewResource      `json:"view"`
	Streams    []StreamState     `json:"streams,omitempty"`
	Prediction PredictionState   `json:"prediction"`
}

// CapturePlayerSide reads it. Caller MUST hold updateMutex.
func (w *World) CapturePlayerSide() PlayerSide {
	r := w.Resources
	return PlayerSide{
		CameraX: r.Config.CameraX, CameraY: r.Config.CameraY, Pointer: r.Config.pointer,
		APM:        r.Game.State.APM(),
		Explosions: slices.Clone(r.Transient.ExplosionBacking[:r.Transient.ExplosionCount]),
		View:       *r.View,
		Streams:    r.Rand.SaveStreams(core.DomainPlayer),
		Prediction: r.Player.Prediction(),
	}
}

// InstallPlayerSide writes it, reporting any stream this world never issued.
// Caller MUST hold updateMutex.
func (w *World) InstallPlayerSide(s PlayerSide) []string {
	r := w.Resources
	r.Config.CameraX, r.Config.CameraY, r.Config.pointer = s.CameraX, s.CameraY, s.Pointer
	r.Game.State.SetAPM(s.APM)
	r.Transient.ExplosionCount = copy(r.Transient.ExplosionBacking[:], s.Explosions)
	*r.View = s.View
	r.Player.SetPrediction(s.Prediction)
	return r.Rand.LoadStreams(core.DomainPlayer, s.Streams)
}
