package engine

import (
	"cmp"
	"errors"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// EntityClass is the half of a cell an entity's domain places it in.
func EntityClass(e core.Entity) position.Class { return position.Class(e.Domain()) }

// A domain is its class: the shared one first, the player one in the budget.
var _ [position.ClassShared - position.Class(core.DomainShared)]struct{}
var _ [position.ClassPrivate - position.Class(core.DomainPlayer)]struct{}

// Position binds the world's placements to the spatial index it embeds: the rows an
// entity's cell is read from, which shared occupants are walls, the rock, typing's
// view of a cell. The index's writes (Set, RemoveEntityAt, Load, Reorder) move no
// row: a placement goes through SetPosition, Move and Shift.
// SYNC: all Position access occurs under World.updateMutex; no internal locking
type Position struct {
	*position.Index[core.Entity]
	world    *World
	slots    sparseIndex
	dense    []component.PositionComponent
	entities []core.Entity // Dense array for cache-friendly iteration
	wall     []bool        // beside dense: the row's entity is a wall the planes follow
	bit      uint64        // Component bit mask
	rock     *RockField    // the cave's rock, two surfaces of the index, or nil
	batch    PositionBatch // the one batch, begun again once committed
	rows     []int32       // Shift's dense rows between its two phases

	statCellSaturations *atomic.Int64
	statCellOverflows   *atomic.Int64
	statPlayerRejects   *atomic.Int64
	statOccupiedCells   *atomic.Int64
	statIndexedEntities *atomic.Int64
	statIndexedShared   *atomic.Int64
	statUnindexed       *atomic.Int64
	statMaxOccupancy    *atomic.Int64
	statOccupancyHWM    *atomic.Int64
	statPositionsHWM    *atomic.Int64
	statBatchSizeHWM    *atomic.Int64
}

// NewPosition creates a new position store with spatial indexing
func NewPosition(w *World, bit uint64) *Position {
	p := &Position{
		dense:    make([]component.PositionComponent, 0, 64),
		entities: make([]core.Entity, 0, 64),
		world:    w,
		bit:      bit,
	}
	p.Index = position.New[core.Entity](p, p.wallOf)
	return p
}

// Aspect is the cell aspect the searches round on, the config's.
func (p *Position) Aspect() vmath.Aspect { return p.world.Resources.Config.CellAspect }

// wallOf reports whether shared occupant e is a wall, and its kinds: the movers it
// stops and the box style it draws in
func (p *Position) wallOf(e core.Entity) (component.WallBlockMask, bool) {
	wall, ok := p.world.Components.Wall.GetPtr(e)
	if !ok {
		return 0, false
	}
	return wall.BlockMask&component.WallBlockAll | component.BoxKind(wall.BoxStyle), true
}

// BindTelemetry registers spatial diagnostics after GameContext creates the registry.
func (p *Position) BindTelemetry(reg *status.Registry) {
	p.statCellSaturations = reg.Ints.Get("spatial.cell_saturations")
	p.statCellOverflows = reg.Ints.Get("spatial.cell_overflows")
	p.statPlayerRejects = reg.Ints.Get("spatial.player_budget_rejects")
	p.statOccupiedCells = reg.Ints.Get("spatial.occupied_cells")
	p.statIndexedEntities = reg.Ints.Get("spatial.indexed_entities")
	p.statIndexedShared = reg.Ints.Get("spatial.indexed_shared")
	p.statUnindexed = reg.Ints.Get("spatial.unindexed")
	p.statMaxOccupancy = reg.Ints.Get("spatial.max_cell_occupancy")
	p.statOccupancyHWM = reg.Ints.Get("spatial.cell_occupancy_hwm")
	p.statPositionsHWM = reg.Ints.Get("spatial.positions_hwm")
	p.statBatchSizeHWM = reg.Ints.Get("spatial.position_batch_hwm")
	p.ResetTelemetry()
}

// ResetTelemetry clears session counters and derived gauges.
func (p *Position) ResetTelemetry() {
	for _, stat := range []*atomic.Int64{
		p.statCellSaturations,
		p.statCellOverflows,
		p.statPlayerRejects,
		p.statOccupiedCells,
		p.statIndexedEntities,
		p.statIndexedShared,
		p.statUnindexed,
		p.statMaxOccupancy,
		p.statOccupancyHWM,
		p.statPositionsHWM,
		p.statBatchSizeHWM,
	} {
		if stat != nil {
			stat.Store(0)
		}
	}
}

// PublishTelemetry computes grid gauges on the status snapshot cadence.
func (p *Position) PublishTelemetry() {
	if p.statOccupiedCells == nil {
		return
	}
	stats := p.Index.ComputeStats()
	p.statOccupiedCells.Store(int64(stats.CellsOccupied))
	p.statIndexedEntities.Store(int64(stats.EntitiesTotal))
	p.statIndexedShared.Store(int64(stats.EntitiesShared))
	p.statMaxOccupancy.Store(int64(stats.MaxOccupancy))
	// Positioned but held in no cell: off-map after a crop, or dropped by a full
	// cell. Either way nothing renders them and no grid query reaches them.
	p.statUnindexed.Store(int64(len(p.entities) - stats.EntitiesTotal))
}

// SetPosition inserts or updates an entity's position. An already-indexed write
// to the same cell is a no-op; a soft-clipped entity at that cell still retries
// insertion. Multiple entities at one position are allowed, overflow silently ignored.
func (p *Position) SetPosition(e core.Entity, pos component.PositionComponent) {
	countSaturation := true
	i, ok := p.slots.get(e)
	if ok {
		old := p.dense[i]
		if old == pos && p.Index.Holds(e, EntityClass(e), old) {
			return
		}
		countSaturation = old != pos
		p.Index.RemoveEntityAt(e, old)
		p.wallMoved(i, old)
		p.dense[i] = pos
	} else {
		i = p.insert(e, pos)
	}
	p.setGrid(e, pos, countSaturation)
	p.wallMoved(i, pos)
}

// insert gives an entity new to the store its row, leaving the spatial index to
// the caller.
func (p *Position) insert(e core.Entity, pos component.PositionComponent) int32 {
	i := int32(len(p.dense))
	p.slots.set(e, i)
	p.dense = append(p.dense, pos)
	p.entities = append(p.entities, e)
	p.wall = append(p.wall, p.isWall(e))
	p.world.AddComponentMask(e, p.bit)
	storeAtomicMax(p.statPositionsHWM, int64(len(p.entities)))
	return i
}

// setGrid preserves soft clipping while exposing saturation, budget rejection, and
// data loss; it reports whether the cell took the entity.
func (p *Position) setGrid(e core.Entity, pos component.PositionComponent, countSaturation bool) bool {
	class := EntityClass(e)
	held := p.Index.Set(e, class, pos)
	if held == 0 {
		// Off the map is no cell's refusal, and a player insert stopped by its own
		// budget is not cell exhaustion
		if !p.Index.InBounds(pos) {
			return false
		}
		if class == position.ClassPrivate && len(p.Index.EntitiesAt(pos, position.ScopePrivate)) >= position.PrivateBudget {
			if p.statPlayerRejects != nil {
				p.statPlayerRejects.Add(1)
			}
		} else if p.statCellOverflows != nil {
			p.statCellOverflows.Add(1)
		}
		return false
	}
	if p.statOccupancyHWM != nil {
		storeAtomicMax(p.statOccupancyHWM, int64(held))
	}
	if countSaturation && held == position.CellCapacity && p.statCellSaturations != nil {
		p.statCellSaturations.Add(1)
	}
	return true
}

func storeAtomicMax(dst *atomic.Int64, value int64) {
	if dst == nil {
		return
	}
	for old := dst.Load(); value > old; old = dst.Load() {
		if dst.CompareAndSwap(old, value) {
			return
		}
	}
}

// GetPosition retrieves a position component
func (p *Position) GetPosition(e core.Entity) (component.PositionComponent, bool) {
	if i, ok := p.slots.get(e); ok {
		return p.dense[i], true
	}
	return component.PositionComponent{}, false
}

// removeAt swap-removes dense slot i; caller already removed grid entry
func (p *Position) removeAt(i int32) {
	e := p.entities[i]
	last := int32(len(p.dense) - 1)
	if i != last {
		moved := p.entities[last]
		p.dense[i], p.wall[i] = p.dense[last], p.wall[last]
		p.entities[i] = moved
		p.slots.set(moved, i)
	}
	p.dense, p.wall = p.dense[:last], p.wall[:last]
	p.entities = p.entities[:last]
	p.slots.del(e)
}

// RemoveEntity deletes an entity from the store and grid. O(1)
func (p *Position) RemoveEntity(e core.Entity, skipMask ...bool) {
	i, ok := p.slots.get(e)
	if !ok {
		return
	}
	p.remove(e, i)
	if len(skipMask) == 0 || !skipMask[0] {
		p.world.RemoveComponentMask(e, p.bit)
	}
}

// remove takes e, row i, out of the grid and the rows, a wall's cell logged once
// the grid has let it go
func (p *Position) remove(e core.Entity, i int32) {
	pos, wall := p.dense[i], p.wall[i]
	p.Index.RemoveEntityAt(e, pos)
	p.removeAt(i)
	if wall {
		p.Index.BlockChangedAt(pos)
	}
}

// RemoveBatch deletes multiple entities. O(m)
func (p *Position) RemoveBatch(entities []core.Entity, skipMask ...bool) {
	if len(p.entities) == 0 {
		return
	}
	clearMask := len(skipMask) == 0 || !skipMask[0]
	for _, e := range entities {
		i, ok := p.slots.get(e)
		if !ok {
			continue
		}
		p.remove(e, i)
		if clearMask {
			p.world.RemoveComponentMask(e, p.bit)
		}
	}
}

// Resize sizes the grid to the map and places every entity in it again; the
// backing storage is grow-only, so a shrink is cheap
func (p *Position) Resize(width, height int) {
	p.Index.Resize(width, height, 1)

	// Re-populate grid from dense component data
	for i, e := range p.entities {
		// Explicit ignore for OOB and Cell full
		p.setGrid(e, p.dense[i], true)
	}
}

// HasPosition checks if an entity has a position component
func (p *Position) HasPosition(e core.Entity) bool {
	_, ok := p.slots.get(e)
	return ok
}

// AllEntities returns a detached copy of all entities with position components
// Safe to iterate while removing. Prefer Entities() for read-only hot paths
func (p *Position) AllEntities() []core.Entity {
	result := make([]core.Entity, len(p.entities))
	copy(result, p.entities)
	return result
}

// Entities returns the live dense entity slice — zero allocation
// CONTRACT: no removals from this store while ranging it; collect candidates
// and remove after the loop (same contract as Store.Entities)
func (p *Position) Entities() []core.Entity {
	return p.entities
}

// CountEntities returns the number of entities
func (p *Position) CountEntities() int {
	return len(p.entities)
}

// Clear removes every placement, the rock with them, retaining capacity
func (p *Position) Clear() {
	// Component mask will be removed centrally by wipeAll()
	p.slots.clear()
	p.dense, p.wall = p.dense[:0], p.wall[:0]
	p.entities = p.entities[:0]
	p.Index.Clear()
	p.rock = nil
}

// --- Wall ---

// WallChanged logs the cell of e, whose wall component came, went or was rewritten
// by a writer no store sees: a BlockMask set in place, a component overwritten.
func (p *Position) WallChanged(e core.Entity) {
	if i, ok := p.slots.get(e); ok {
		p.wall[i] = p.isWall(e)
		p.Index.BlockChangedAt(p.dense[i])
	}
}

// isWall reports whether e's moves are a wall's: a shared entity the wall store
// holds. A row keeps the answer from when it was made or its wall last changed,
// since a destroy empties the wall store before the row goes.
func (p *Position) isWall(e core.Entity) bool {
	return e.Domain() != core.DomainPlayer && p.world.Components.Wall.HasEntity(e)
}

// readWalls sets every row's wall flag again, after rows were written whole
func (p *Position) readWalls() {
	p.wall = p.wall[:0]
	for _, e := range p.entities {
		p.wall = append(p.wall, p.isWall(e))
	}
}

// wallMoved logs pos once the wall of row i, if it is one, has left or entered it
// in the grid
func (p *Position) wallMoved(i int32, pos component.PositionComponent) {
	if p.wall[i] {
		p.Index.BlockChangedAt(pos)
	}
}

// Move updates the position of an existing entity; it is a no-op when the entity
// has no position. Position access is serialized by World.updateMutex.
func (p *Position) Move(e core.Entity, newPos component.PositionComponent) {
	i, ok := p.slots.get(e)
	if !ok {
		return
	}
	old := p.dense[i]
	p.Index.RemoveEntityAt(e, old)
	p.wallMoved(i, old)
	p.dense[i] = newPos
	// Explicit ignore for OOB and Cell full
	p.setGrid(e, newPos, old != newPos)
	p.wallMoved(i, newPos)
}

// Shift translates every entity by (dx, dy) in two phases, all leaving their cells
// before any enters one, so a rigid group never contends with itself for a cell's
// budget. Dense order is kept. Entities must be unique; those the destination cell
// refused are appended to unplaced, which is returned.
func (p *Position) Shift(entities []core.Entity, dx, dy int, unplaced []core.Entity) []core.Entity {
	if dx == 0 && dy == 0 {
		return unplaced
	}
	rows := p.rows[:0]
	for _, e := range entities {
		i, ok := p.slots.get(e)
		if ok {
			old := p.dense[i]
			p.Index.RemoveEntityAt(e, old)
			p.wallMoved(i, old)
			p.dense[i] = component.PositionComponent{X: old.X + dx, Y: old.Y + dy}
		} else {
			i = -1
		}
		rows = append(rows, i)
	}
	for k, i := range rows {
		if i < 0 {
			continue
		}
		if !p.setGrid(entities[k], p.dense[i], true) {
			unplaced = append(unplaced, entities[k])
		}
		p.wallMoved(i, p.dense[i])
	}
	p.rows = rows
	return unplaced
}

// TopGlyphAt returns the one glyph at (x, y) that typing acts on: shared (gold)
// before player, then the nearer level, then the lower entity, passing over
// scenery. It reads the grid, so a glyph its cell refused is never on top.
func (p *Position) TopGlyphAt(x, y int) (core.Entity, bool) {
	top := p.topGlyph(x, y)
	return top, top != 0
}

// TopGlyphIs reports whether typeable e is the glyph drawn at (x, y), the one
// typing acts on. Alone in its cell it is, which spares the lookup.
func (p *Position) TopGlyphIs(e core.Entity, x, y int) bool {
	if cell := p.Index.Occupants(component.PositionComponent{X: x, Y: y}, position.ScopeAll); len(cell) == 1 {
		return cell[0] == e
	}
	return p.topGlyph(x, y) == e
}

// IsTypeable reports whether e is a glyph typing can act on: one not placed as
// scenery
func (p *Position) IsTypeable(e core.Entity) bool {
	return p.world.Components.Glyph.HasEntity(e) && !p.IsScenery(e)
}

// IsScenery reports whether e was placed as scenery (ProtectFromTyping), for a
// caller that already knows e is a glyph
func (p *Position) IsScenery(e core.Entity) bool {
	prot, _ := p.world.Components.Protection.GetComponent(e)
	return prot.Mask&component.ProtectFromTyping != 0
}

// topGlyph ranks the typeable glyphs at (x, y), passing over scenery
func (p *Position) topGlyph(x, y int) core.Entity {
	glyphs := p.world.Components.Glyph
	var top core.Entity
	var topLevel component.GlyphLevel
	for _, e := range p.Index.Occupants(component.PositionComponent{X: x, Y: y}, position.ScopeAll) {
		g, ok := glyphs.GetPtr(e)
		if !ok || p.IsScenery(e) {
			continue
		}
		if top != 0 && (e.Domain() > top.Domain() || e.Domain() == top.Domain() &&
			(g.Level < topLevel || g.Level == topLevel && e > top)) {
			continue
		}
		top, topLevel = e, g.Level
	}
	return top
}

// --- Batch Implementation ---

// PositionBatch stages placements to check and apply together. A store keeps one,
// in storage it reuses: a batch is committed before the next begins.
type PositionBatch struct {
	store     *Position
	additions []positionAddition
	cells     []component.PositionComponent // commit's: the additions' cells, sorted
	committed bool
}

type positionAddition struct {
	entity core.Entity
	pos    component.PositionComponent
}

var (
	errBatchCommitted = errors.New("batch already committed")
	errBatchOccupied  = errors.New("position is occupied")
	errBatchConflict  = errors.New("batch conflict at position")
)

// BeginBatch starts the store's batch again, with room for n additions.
func (p *Position) BeginBatch(n int) *PositionBatch {
	b := &p.batch
	b.store, b.additions, b.committed = p, slices.Grow(b.additions[:0], n), false
	return b
}

func (pb *PositionBatch) Add(e core.Entity, pos component.PositionComponent) {
	pb.additions = append(pb.additions, positionAddition{entity: e, pos: pos})
}

// Commit applies all batched additions, rejecting a cell occupied by any entity
func (pb *PositionBatch) Commit() error {
	return pb.commit(position.ScopeAll)
}

// CommitShared applies all batched additions, rejecting only shared occupancy so a
// player glyph or dust pile cannot veto a shared placement.
func (pb *PositionBatch) CommitShared() error {
	return pb.commit(position.ScopeShared)
}

// commit validates every addition against the occupants in scope, then applies the batch
func (pb *PositionBatch) commit(scope position.Scope) error {
	if pb.committed {
		return errBatchCommitted
	}
	pb.committed = true
	storeAtomicMax(pb.store.statBatchSizeHWM, int64(len(pb.additions)))

	// Every addition against the world, then against the rest of the batch: two on
	// one cell sort together
	cells := pb.cells[:0]
	for _, add := range pb.additions {
		if pb.store.HasAnyEntityAt(add.pos, scope) {
			return errBatchOccupied
		}
		cells = append(cells, add.pos)
	}
	pb.cells = cells
	slices.SortFunc(cells, func(a, b component.PositionComponent) int {
		return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	for i := 1; i < len(cells); i++ {
		if cells[i] == cells[i-1] {
			return errBatchConflict
		}
	}

	for _, add := range pb.additions {
		pb.store.SetPosition(add.entity, add.pos)
	}
	return nil
}

// CommitForce applies batch additions without checking for existing entity collisions
// Used for effects like Dust that overlay existing entities or replace them before death processing
func (pb *PositionBatch) CommitForce() {
	if pb.committed {
		return
	}
	pb.committed = true
	storeAtomicMax(pb.store.statBatchSizeHWM, int64(len(pb.additions)))

	for _, add := range pb.additions {
		pb.store.SetPosition(add.entity, add.pos)
	}
}
