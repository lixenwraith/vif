package engine

import (
	"fmt"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// The names the game reads the package's types by.
type (
	DomainScope = position.Scope
	Cell        = position.Cell[core.Entity]
	SpatialGrid = position.Grid[core.Entity]
	GridStats   = position.Stats
	CellOrder   = position.CellOrder[core.Entity]
	Exclusion   = position.Exclusion
	PatternType = position.Pattern
)

const (
	ScopeShared = position.ScopeShared
	ScopePlayer = position.ScopePrivate
	ScopeBoth   = position.ScopeAll

	PatternCardinalFirst = position.PatternCardinalFirst
	PatternDiagonalFirst = position.PatternDiagonalFirst
)

// EntityClass is the half of a cell an entity's domain places it in.
func EntityClass(e core.Entity) position.Class { return position.Class(e.Domain()) }

// A domain is its class: the shared one first, the player one in the budget.
var _ [position.ClassShared - position.Class(core.DomainShared)]struct{}
var _ [position.ClassPrivate - position.Class(core.DomainPlayer)]struct{}

// Position binds the world's placements to the spatial index: the rows an entity's
// cell is read from, the index that holds the cells and answers every geometry
// query, which shared occupants are walls, and typing's view of a cell.
// SYNC: all Position access occurs under World.updateMutex; no internal locking
type Position struct {
	world    *World
	index    *position.Index[core.Entity]
	slots    sparseIndex
	dense    []component.PositionComponent
	entities []core.Entity // Dense array for cache-friendly iteration
	bit      uint64        // Component bit mask
	rock     *RockField    // the cave's rock, two surfaces of the index, or nil
	rockLook RockLook      // how the rock field draws, kept beside it

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
	p.index = position.New[core.Entity](p, p.wallOf)
	return p
}

// Aspect is the cell aspect the searches round on, the config's.
func (p *Position) Aspect() vmath.Aspect { return p.world.Resources.Config.CellAspect }

// wallOf reports whether shared occupant e is a wall, and what it stops
func (p *Position) wallOf(e core.Entity) (component.WallBlockMask, bool) {
	wall, ok := p.world.Components.Wall.GetPtr(e)
	if !ok {
		return 0, false
	}
	return wall.BlockMask, true
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
	stats := p.index.ComputeStats()
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
	if i, ok := p.slots.get(e); ok {
		old := p.dense[i]
		if old == pos && p.index.Holds(e, EntityClass(e), old.X, old.Y) {
			return
		}
		countSaturation = old.X != pos.X || old.Y != pos.Y
		p.index.RemoveEntityAt(e, old.X, old.Y)
		p.wallMoved(e, old.X, old.Y)
		p.dense[i] = pos
	} else {
		p.insert(e, pos)
	}
	p.setGrid(e, pos.X, pos.Y, countSaturation)
	p.wallMoved(e, pos.X, pos.Y)
}

// insert places an entity new to the store, leaving the spatial index to the caller.
func (p *Position) insert(e core.Entity, pos component.PositionComponent) {
	p.slots.set(e, int32(len(p.dense)))
	p.dense = append(p.dense, pos)
	p.entities = append(p.entities, e)
	p.world.AddComponentMask(e, p.bit)
	storeAtomicMax(p.statPositionsHWM, int64(len(p.entities)))
}

// setGrid preserves soft clipping while exposing saturation, budget rejection, and
// data loss; it reports whether the cell took the entity.
func (p *Position) setGrid(e core.Entity, x, y int, countSaturation bool) bool {
	class := EntityClass(e)
	if x < 0 || x >= p.index.Width || y < 0 || y >= p.index.Height {
		return p.index.Set(e, class, x, y)
	}
	cell := &p.index.Cells[y*p.index.Width+x]
	before := cell.Count
	playerBefore := cell.Count - cell.SharedCount
	if !p.index.Set(e, class, x, y) {
		// A player insert stopped by its own budget is not cell exhaustion
		if class == position.ClassPrivate && playerBefore >= position.PrivateBudget {
			if p.statPlayerRejects != nil {
				p.statPlayerRejects.Add(1)
			}
		} else if p.statCellOverflows != nil {
			p.statCellOverflows.Add(1)
		}
		return false
	}
	if p.statOccupancyHWM != nil {
		storeAtomicMax(p.statOccupancyHWM, int64(before+1))
	}
	if countSaturation && before+1 == position.CellCapacity && p.statCellSaturations != nil {
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
		p.dense[i] = p.dense[last]
		p.entities[i] = moved
		p.slots.set(moved, i)
	}
	p.dense = p.dense[:last]
	p.entities = p.entities[:last]
	p.slots.del(e)
}

// RemoveEntity deletes an entity from the store and grid. O(1)
func (p *Position) RemoveEntity(e core.Entity, skipMask ...bool) {
	i, ok := p.slots.get(e)
	if !ok {
		return
	}
	pos := p.dense[i]
	p.index.RemoveEntityAt(e, pos.X, pos.Y)
	p.removeAt(i)
	p.wallMoved(e, pos.X, pos.Y)
	if len(skipMask) == 0 || !skipMask[0] {
		p.world.RemoveComponentMask(e, p.bit)
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
		pos := p.dense[i]
		p.index.RemoveEntityAt(e, pos.X, pos.Y)
		p.removeAt(i)
		p.wallMoved(e, pos.X, pos.Y)
		if clearMask {
			p.world.RemoveComponentMask(e, p.bit)
		}
	}
}

// GetEntitiesAt returns a COPY of the in-scope entities at (x, y), nil if OOB or empty
// Copy is required: callers may SetPosition/remove returned entities while
// iterating, which swap-mutates the underlying grid cell.
// Hot paths should prefer GetEntitiesAtInto (zero-alloc)
func (p *Position) GetEntitiesAt(x, y int, scope DomainScope) []core.Entity {
	view := p.index.EntitiesAt(x, y, scope)
	if len(view) == 0 {
		return nil
	}

	// Allocate new slice to detach from grid memory
	result := make([]core.Entity, len(view))
	copy(result, view)
	return result
}

// GetAllEntityAt returns a COPY of every entity at (x, y) regardless of domain
func (p *Position) GetAllEntityAt(x, y int) []core.Entity {
	return p.GetEntitiesAt(x, y, ScopeBoth)
}

// GetEntitiesAtInto copies in-scope entities at (x,y) into a caller-provided buffer and returns number copied, Zero-alloc if buf is on stack
func (p *Position) GetEntitiesAtInto(x, y int, scope DomainScope, buf []core.Entity) int {
	view := p.index.EntitiesAt(x, y, scope)
	count := min(len(view), len(buf))
	copy(buf, view[:count])
	return count
}

// GetAllEntitiesAtInto copies every entity at (x,y) regardless of domain
func (p *Position) GetAllEntitiesAtInto(x, y int, buf []core.Entity) int {
	return p.GetEntitiesAtInto(x, y, ScopeBoth, buf)
}

// HasAnyEntityAt O(1) returns true if any entity of either domain exists at (x, y)
func (p *Position) HasAnyEntityAt(x, y int) bool {
	return p.index.HasAnyEntityAt(x, y, ScopeBoth)
}

// HasAnySharedEntityAt O(1) returns true if a shared entity occupies (x, y)
func (p *Position) HasAnySharedEntityAt(x, y int) bool {
	return p.index.HasAnyEntityAt(x, y, ScopeShared)
}

// ResizeGrid resizes the internal spatial grid and re-indexes all entities
// Backing storage is grow-only (Grid.Resize); cheap on shrink
func (p *Position) ResizeGrid(width, height int) {
	p.index.Resize(width, height)

	// Re-populate grid from dense component data
	for i, e := range p.entities {
		// Explicit ignore for OOB and Cell full
		p.setGrid(e, p.dense[i].X, p.dense[i].Y, true)
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

// ClearAllComponents removes all data, retaining capacity
func (p *Position) ClearAllComponents() {
	// Component mask will be removed centrally by wipeAll()
	p.slots.clear()
	p.dense = p.dense[:0]
	p.entities = p.entities[:0]
	p.index.Clear()
	p.rock, p.rockLook = nil, RockLook{}
}

// --- Wall ---

// WallBits returns HasBlockingWallAt for mask as a grid of bits in buf's storage,
// exact while walls cannot change.
// SYNC: caller holds World.updateMutex for as long as it reads the result
func (p *Position) WallBits(mask component.WallBlockMask, buf []uint64) position.Bits {
	return p.index.BlockBits(mask, buf)
}

// WallsChanged moves the wall epoch for walls rewritten wholesale, an install's,
// and loses the wall log, since it names no cells.
func (p *Position) WallsChanged() { p.index.BlocksChanged() }

// WallChanged logs the cell of e, a wall a writer no store sees rewrote: a
// BlockMask set in place, or a wall component overwritten.
func (p *Position) WallChanged(e core.Entity) {
	if i, ok := p.slots.get(e); ok {
		p.index.BlockChangedAt(p.dense[i].X, p.dense[i].Y)
	}
}

// wallMoved logs (x, y) once e, a wall a grid can hold, has left or entered it in
// the grid. The mask, not the wall store, names a wall: a destroy empties the
// store first.
func (p *Position) wallMoved(e core.Entity, x, y int) {
	if e.Domain() != core.DomainPlayer && p.world.componentMask[e]&WallBit != 0 {
		p.index.BlockChangedAt(x, y)
	}
}

// WallChanges returns every cell whose HasBlockingWallAt may answer otherwise than
// it did at sequence since, and the sequence it stands at now; see Index.WallChanges.
// SYNC: caller holds World.updateMutex
func (p *Position) WallChanges(since uint64) (cells []vmath.Point, now uint64, ok bool) {
	return p.index.BlockChanges(since)
}

// HasBlockingWallAt returns true if a wall exists at (x, y) that blocks the given mask
// SYNC: caller holds World.updateMutex
func (p *Position) HasBlockingWallAt(x, y int, mask component.WallBlockMask) bool {
	return p.index.Blocks(x, y, mask)
}

// HasBlockingWallInArea returns true if any wall exists in rectangular area that blocks the given mask
// Area defined as [x, x+width) × [y, y+height), skips out-of-bounds cells
func (p *Position) HasBlockingWallInArea(x, y, width, height int, mask component.WallBlockMask) bool {
	return p.index.BlocksArea(x, y, width, height, mask)
}

// HasBlockingWallInAreaUnsafe is HasBlockingWallInArea; the name is kept for its callers
func (p *Position) HasBlockingWallInAreaUnsafe(x, y, width, height int, mask component.WallBlockMask) bool {
	return p.HasBlockingWallInArea(x, y, width, height, mask)
}

// FindFreeAreaSpiral searches outward from origin for a width×height area free of
// walls matching mask; see Index.FindFreeAreaSpiral.
func (p *Position) FindFreeAreaSpiral(
	originX, originY int,
	width, height int,
	anchorOffsetX, anchorOffsetY int,
	mask component.WallBlockMask,
	maxRadius int,
) (int, int, bool) {
	return p.index.FindFreeAreaSpiral(originX, originY, width, height, anchorOffsetX, anchorOffsetY, mask, maxRadius)
}

// SpiralSearch is FindFreeAreaSpiral's walk for a footprint no rectangle holds; see
// Index.SpiralSearch.
func (p *Position) SpiralSearch(originX, originY, maxRadius int, fits func(x, y int) bool) (int, int, bool) {
	return p.index.SpiralSearch(originX, originY, maxRadius, fits)
}

// FindFreeAreaInside is FindFreeAreaSpiral from the origin pulled in from the map's
// edges; see Index.FindFreeAreaInside.
func (p *Position) FindFreeAreaInside(
	originX, originY int,
	width, height int,
	anchorOffsetX, anchorOffsetY int,
	mask component.WallBlockMask,
	maxRadius int,
) (int, int, bool) {
	return p.index.FindFreeAreaInside(originX, originY, width, height, anchorOffsetX, anchorOffsetY, mask, maxRadius)
}

// MapSearchRadius is the spiral radius whose rays reach every column and every row
// of the map from any cell in it
func (p *Position) MapSearchRadius() int { return p.index.MapSearchRadius() }

// IsAreaFree checks if the rectangular area is strictly within grid bounds and free of blocking walls
func (p *Position) IsAreaFree(x, y, width, height int, mask component.WallBlockMask) bool {
	return p.index.IsAreaFree(x, y, width, height, mask)
}

// IsBlocked checks if a specific point is invalid (OOB) or blocked by a wall
func (p *Position) IsBlocked(x, y int, mask component.WallBlockMask) bool {
	return p.index.IsBlocked(x, y, mask)
}

// IsOutOfBounds checks if position is outside the map
func (p *Position) IsOutOfBounds(x, y int) bool { return p.index.IsOutOfBounds(x, y) }

// CheckBlockedBatch checks multiple points for blocking (OOB or wall)
// Returns bool slice aligned with input where true = position is blocked
func (p *Position) CheckBlockedBatch(points []vmath.Point, mask component.WallBlockMask) []bool {
	return p.index.CheckBlockedBatch(points, mask)
}

// IsAnyBlockedInSet returns true if any point is blocked (OOB or wall)
func (p *Position) IsAnyBlockedInSet(points []vmath.Point, mask component.WallBlockMask) bool {
	return p.index.IsAnyBlockedInSet(points, mask)
}

// HasLineOfSight checks if two grid points have unobstructed line of sight
func (p *Position) HasLineOfSight(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	return p.index.HasLineOfSight(x0, y0, x1, y1, mask)
}

// HasLineOfSightUnsafe is HasLineOfSight; the name is kept for its callers
func (p *Position) HasLineOfSightUnsafe(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	return p.HasLineOfSight(x0, y0, x1, y1, mask)
}

// Move updates the position of an existing entity; it is a no-op when the entity
// has no position. Position access is serialized by World.updateMutex.
func (p *Position) Move(e core.Entity, newPos component.PositionComponent) {
	i, ok := p.slots.get(e)
	if !ok {
		return
	}
	old := p.dense[i]
	p.index.RemoveEntityAt(e, old.X, old.Y)
	p.wallMoved(e, old.X, old.Y)
	p.dense[i] = newPos
	// Explicit ignore for OOB and Cell full
	p.setGrid(e, newPos.X, newPos.Y, old.X != newPos.X || old.Y != newPos.Y)
	p.wallMoved(e, newPos.X, newPos.Y)
}

// Shift translates every entity by (dx, dy) in two phases, all leaving their cells
// before any enters one, so a rigid group never contends with itself for a cell's
// budget. Dense order is kept. Entities must be unique; those the destination cell
// refused are appended to unplaced, which is returned.
func (p *Position) Shift(entities []core.Entity, dx, dy int, unplaced []core.Entity) []core.Entity {
	if dx == 0 && dy == 0 {
		return unplaced
	}
	for _, e := range entities {
		if i, ok := p.slots.get(e); ok {
			old := p.dense[i]
			p.index.RemoveEntityAt(e, old.X, old.Y)
			p.wallMoved(e, old.X, old.Y)
			p.dense[i] = component.PositionComponent{X: old.X + dx, Y: old.Y + dy}
		}
	}
	for _, e := range entities {
		if i, ok := p.slots.get(e); ok {
			if !p.setGrid(e, p.dense[i].X, p.dense[i].Y, true) {
				unplaced = append(unplaced, e)
			}
			p.wallMoved(e, p.dense[i].X, p.dense[i].Y)
		}
	}
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
	if cell := p.index.Occupants(x, y, ScopeBoth); len(cell) == 1 {
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
	for _, e := range p.index.Occupants(x, y, ScopeBoth) {
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

type PositionBatch struct {
	store     *Position
	additions []positionAddition
	committed bool
}

type positionAddition struct {
	entity core.Entity
	pos    component.PositionComponent
}

// BeginBatch starts a batch with room for n additions.
func (p *Position) BeginBatch(n int) *PositionBatch {
	return &PositionBatch{
		store:     p,
		additions: make([]positionAddition, 0, n),
	}
}

func (pb *PositionBatch) Add(e core.Entity, pos component.PositionComponent) {
	pb.additions = append(pb.additions, positionAddition{entity: e, pos: pos})
}

// Commit applies all batched additions, rejecting a cell occupied by any entity
func (pb *PositionBatch) Commit() error {
	return pb.commit(pb.store.HasAnyEntityAt)
}

// CommitShared applies all batched additions, rejecting only shared occupancy so a
// player glyph or dust pile cannot veto a shared placement.
func (pb *PositionBatch) CommitShared() error {
	return pb.commit(pb.store.HasAnySharedEntityAt)
}

// commit validates every addition against an occupancy gate, then applies the batch
func (pb *PositionBatch) commit(occupied func(x, y int) bool) error {
	if pb.committed {
		return fmt.Errorf("batch already committed")
	}
	pb.committed = true
	storeAtomicMax(pb.store.statBatchSizeHWM, int64(len(pb.additions)))

	// Every addition against the world and against the rest of the batch
	batchOccupied := make(map[int]map[int]bool)
	for _, add := range pb.additions {
		if occupied(add.pos.X, add.pos.Y) {
			return fmt.Errorf("position is occupied")
		}
		if batchOccupied[add.pos.Y] == nil {
			batchOccupied[add.pos.Y] = make(map[int]bool)
		}
		if batchOccupied[add.pos.Y][add.pos.X] {
			return fmt.Errorf("batch conflict at position")
		}
		batchOccupied[add.pos.Y][add.pos.X] = true
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

// GridStats returns computed statistics for the spatial grid
func (p *Position) GridStats() GridStats {
	return p.index.ComputeStats()
}

// GridDimensions returns width and height of the spatial grid
func (p *Position) GridDimensions() (width, height int) {
	return p.index.Width, p.index.Height
}

// FindClosestEntityInDirection searches for entities in a cardinal direction (up,
// down, left, right) within the specified bounds; see Index.FindClosestEntityInDirection.
func (p *Position) FindClosestEntityInDirection(startX, startY, dx, dy int, bounds PingAbsoluteBounds, scope DomainScope, filter func(core.Entity) bool) (core.Entity, int, int, bool) {
	return p.index.FindClosestEntityInDirection(startX, startY, dx, dy,
		position.Rect{MinX: bounds.MinX, MaxX: bounds.MaxX, MinY: bounds.MinY, MaxY: bounds.MaxY}, scope, filter)
}

// FindFreeFromPattern searches 8 directions at expanding radii for free area; see
// Index.FindFreeFromPattern.
func (p *Position) FindFreeFromPattern(
	originX, originY int,
	width, height int,
	pattern PatternType,
	startRadius, maxRadius int,
	aspectCorrect bool,
	mask component.WallBlockMask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	return p.index.FindFreeFromPattern(originX, originY, width, height, pattern, startRadius, maxRadius,
		aspectCorrect, mask, additionalCheck)
}

// FindPlacementAroundExclusion finds valid position for object outside exclusion
// zone, as an offset from the anchor; see Index.FindPlacementAroundExclusion.
func (p *Position) FindPlacementAroundExclusion(
	anchorX, anchorY int,
	objectW, objectH int,
	exclusion Exclusion,
	padding int,
	pattern PatternType,
	mask component.WallBlockMask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	return p.index.FindPlacementAroundExclusion(anchorX, anchorY, objectW, objectH, exclusion, padding, pattern,
		mask, additionalCheck)
}

// FindLastFreeOnRay returns the last unblocked cell on a ray from (startX, startY) toward (endX, endY)
// Returns (x, y, reachedEnd) where reachedEnd=true if entire path is free
func (p *Position) FindLastFreeOnRay(startX, startY, endX, endY int, mask component.WallBlockMask) (int, int, bool) {
	return p.index.FindLastFreeOnRay(startX, startY, endX, endY, mask)
}

// IsPathBlocked checks if straight line from (x0,y0) to (x1,y1) intersects any blocking wall
// Endpoints are NOT checked - only path between them
func (p *Position) IsPathBlocked(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	return !p.HasLineOfSight(x0, y0, x1, y1, mask)
}

// IsPointValidForOrbit checks if grid point is within bounds and not wall-blocked
func (p *Position) IsPointValidForOrbit(x, y int, mask component.WallBlockMask) bool {
	return !p.IsBlocked(x, y, mask)
}

// HasAreaLineOfSight checks if rectangular entity can traverse unobstructed from (x0,y0) to (x1,y1)
func (p *Position) HasAreaLineOfSight(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	return p.index.HasAreaLineOfSight(x0, y0, x1, y1, width, height, mask)
}

// HasAreaLineOfSightUnsafe is HasAreaLineOfSight; the name is kept for its callers
func (p *Position) HasAreaLineOfSightUnsafe(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	return p.HasAreaLineOfSight(x0, y0, x1, y1, width, height, mask)
}

// HasAreaLineOfSightRotatable checks LOS with optional 90° rotation
// Tries width×height first, then height×width if blocked
func (p *Position) HasAreaLineOfSightRotatable(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	return p.index.HasAreaLineOfSightRotatable(x0, y0, x1, y1, width, height, mask)
}
