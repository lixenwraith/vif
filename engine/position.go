package engine

import (
	"fmt"
	"math"
	"sync"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Position maintains a spatial index using a fixed-capacity dense grid, multiple entities per cell (up to MaxEntitiesPerCell)
type Position struct {
	mu         sync.RWMutex
	components map[core.Entity]component.PositionComponent
	entities   []core.Entity // Dense array for cache-friendly iteration
	grid       *SpatialGrid
	world      *World // Reference for z-index lookups
}

// NewPosition creates a new position store with spatial indexing
func NewPosition() *Position {
	// Default grid size, will be resized by GameContext if needed
	return &Position{
		components: make(map[core.Entity]component.PositionComponent),
		entities:   make([]core.Entity, 0, 64),
		grid:       NewSpatialGrid(parameter.DefaultGridWidth, parameter.DefaultGridHeight), // Default safe size
	}
}

// SetPosition inserts or updates an entity's position, multiple entities at one position are allowed, overflow silently ignored
func (p *Position) SetPosition(e core.Entity, pos component.PositionComponent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If entity already has a position, remove it from old grid location
	if oldPos, exists := p.components[e]; exists {
		p.grid.RemoveEntityAt(e, oldPos.X, oldPos.Y)
	} else {
		// New entity, add to dense array
		p.entities = append(p.entities, e)
	}

	// Update component
	p.components[e] = pos

	// Set to new grid location
	_ = p.grid.Set(e, pos.X, pos.Y)
}

// RemoveEntity deletes an entity from the store and grid
func (p *Position) RemoveEntity(e core.Entity) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pos, exists := p.components[e]; exists {
		// RemoveEntity from spatial grid
		p.grid.RemoveEntityAt(e, pos.X, pos.Y)

		// RemoveEntity from components map
		delete(p.components, e)

		// RemoveEntity from dense entities array
		for i, entity := range p.entities {
			if entity == e {
				p.entities[i] = p.entities[len(p.entities)-1]
				p.entities = p.entities[:len(p.entities)-1]
				break
			}
		}
	}
}

// MoveEntity updates position atomically
func (p *Position) MoveEntity(e core.Entity, newPos component.PositionComponent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	oldPos, exists := p.components[e]
	if !exists {
		return fmt.Errorf("entity %d does not have a position component", e)
	}

	// RemoveEntity from old grid pos
	p.grid.RemoveEntityAt(e, oldPos.X, oldPos.Y)

	// Update component
	p.components[e] = newPos

	// Set to new grid pos
	// Explicit ignore for OOB and Cell full
	_ = p.grid.Set(e, newPos.X, newPos.Y)

	return nil
}

// GetAllEntityAt returns a COPY of entities at the given position (concurrent safe but uses memory), nil if OOB or empty
func (p *Position) GetAllEntityAt(x, y int) []core.Entity {
	p.mu.RLock()
	defer p.mu.RUnlock()

	view := p.grid.GetAllEntitiesAt(x, y)
	if len(view) == 0 {
		return nil
	}

	// Allocate new slice to detach from grid memory
	result := make([]core.Entity, len(view))
	copy(result, view)
	return result
}

// GetAllEntitiesAtInto copies entities into a caller-provided buffer and returns number of entities copied, Zero-alloc if buf is on stack
func (p *Position) GetAllEntitiesAtInto(x, y int, buf []core.Entity) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	view := p.grid.GetAllEntitiesAt(x, y)
	// Copy min(len(buf), len(view))
	return copy(buf, view)
}

// HasAnyEntityAt O(1) returns true if any entity exists at the given coordinates
func (p *Position) HasAnyEntityAt(x, y int) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.grid.HasAnyEntityAt(x, y)
}

// ResizeGrid resizes the internal spatial grid
func (p *Position) ResizeGrid(width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create new grid
	p.grid.Resize(width, height)

	// Re-populate grid from components map
	// This ensures consistency even if grid size changes
	for e, pos := range p.components {
		// Explicit ignore for OOB and Cell full
		_ = p.grid.Set(e, pos.X, pos.Y)
	}
}

// GetPosition retrieves a position component
func (p *Position) GetPosition(e core.Entity) (component.PositionComponent, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	val, ok := p.components[e]
	return val, ok
}

// HasPosition checks if an entity has a position component
func (p *Position) HasPosition(e core.Entity) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, ok := p.components[e]
	return ok
}

// AllEntities returns all entities with position components
func (p *Position) AllEntities() []core.Entity {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]core.Entity, len(p.entities))
	copy(result, p.entities)
	return result
}

// CountEntities returns the number of entities
func (p *Position) CountEntities() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entities)
}

// ClearAllComponents removes all data
func (p *Position) ClearAllComponents() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.components = make(map[core.Entity]component.PositionComponent)
	p.entities = make([]core.Entity, 0, 64)
	p.grid.Clear()
}

// SetWorld sets the world reference for z-index lookups
func (p *Position) SetWorld(w *World) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.world = w
}

// --- Wall ---

// HasBlockingWallAt returns true if a wall exists at (x, y) that blocks the given mask
// O(k) where k = entities at cell (typically 1-3)
func (p *Position) HasBlockingWallAt(x, y int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.HasBlockingWallAtUnsafe(x, y, mask)
}

// HasBlockingWallAtUnsafe checks wall without acquiring lock
// Caller MUST hold Lock() or RLock()
func (p *Position) HasBlockingWallAtUnsafe(x, y int, mask component.WallBlockMask) bool {
	if p.world == nil {
		return false
	}

	// Check against Map bounds
	config := p.world.Resources.Config
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}

	idx := y*p.grid.Width + x
	cell := &p.grid.Cells[idx]
	for i := uint8(0); i < cell.Count; i++ {
		if wall, ok := p.world.Components.Wall.GetComponent(cell.Entities[i]); ok {
			// If mask is 0, allow any wall. Otherwise check mask
			if mask == 0 || wall.BlockMask&mask != 0 {
				return true
			}
		}
	}
	return false
}

// HasBlockingWallInArea returns true if any wall exists in rectangular area that blocks the given mask
// Area defined as [x, x+width) × [y, y+height), skips out-of-bounds cells
func (p *Position) HasBlockingWallInArea(x, y, width, height int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.HasBlockingWallInAreaUnsafe(x, y, width, height, mask)
}

// HasBlockingWallInAreaUnsafe checks area for walls without locking
func (p *Position) HasBlockingWallInAreaUnsafe(x, y, width, height int, mask component.WallBlockMask) bool {
	if p.world == nil {
		return false
	}

	return p.grid.HasAnyEntityInArea(x, y, width, height, func(e core.Entity) bool {
		if wall, ok := p.world.Components.Wall.GetComponent(e); ok {
			return mask == 0 || wall.BlockMask&mask != 0
		}
		return false
	})
}

// SpiralSearchDirs defines direction vectors for spiral area search
// Counter-clockwise from top: Top, Top-left, Left, Bottom-left, Bottom, Bottom-right, Right, Top-right
var SpiralSearchDirs = [8][2]int{
	{0, -1}, {-1, -1}, {-1, 0}, {-1, 1}, {0, 1}, {1, 1}, {1, 0}, {1, -1},
}

// FindFreeAreaSpiral searches outward from origin for an area free of blocking walls
// Uses 45° spiral pattern, counter-clockwise from top
// Returns (topLeftX, topLeftY, found) where found=false if no valid position exists
//
// Parameters:
//   - originX, originY: search center (e.g., centroid)
//   - width, height: area dimensions
//   - anchorOffsetX, anchorOffsetY: offset from area top-left to anchor/header position
//   - mask: wall block mask to check (0 = any wall)
//   - maxRadius: maximum search distance in cells (0 = default 20)
func (p *Position) FindFreeAreaSpiral(
	originX, originY int,
	width, height int,
	anchorOffsetX, anchorOffsetY int,
	mask component.WallBlockMask,
	maxRadius int,
) (int, int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.world == nil {
		return 0, 0, false
	}

	if maxRadius <= 0 {
		maxRadius = 20
	}

	// Check origin first (radius 0)
	topLeftX := originX - anchorOffsetX
	topLeftY := originY - anchorOffsetY
	if p.isAreaFreeUnsafe(topLeftX, topLeftY, width, height, mask) {
		return topLeftX, topLeftY, true
	}

	// Spiral outward, checking 8 directions per radius
	// Aspect ratio: terminal cells ~1:2, halve vertical distance for visual uniformity
	for radius := 1; radius <= maxRadius; radius++ {
		vertRadius := (radius + 1) / 2
		for _, dir := range SpiralSearchDirs {
			checkX := originX + dir[0]*radius
			checkY := originY + dir[1]*vertRadius

			topLeftX = checkX - anchorOffsetX
			topLeftY = checkY - anchorOffsetY

			if p.isAreaFreeUnsafe(topLeftX, topLeftY, width, height, mask) {
				return topLeftX, topLeftY, true
			}
		}
	}

	return 0, 0, false
}

// IsAreaFree checks if the rectangular area is strictly within grid bounds and free of blocking walls
// Returns true only if the entire area is valid and empty of walls matching the mask
func (p *Position) IsAreaFree(x, y, width, height int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.isAreaFreeUnsafe(x, y, width, height, mask)
}

// IsBlocked checks if a specific point is invalid (OOB) or blocked by a wall
// Consolidates IsOutOfBounds and HasBlockingWallAt for point entities
func (p *Position) IsBlocked(x, y int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.IsOutOfBounds(x, y) {
		return true
	}
	return p.HasBlockingWallAtUnsafe(x, y, mask)
}

// isAreaFreeUnsafe checks bounds and wall presence, caller must hold lock
func (p *Position) isAreaFreeUnsafe(x, y, width, height int, mask component.WallBlockMask) bool {
	config := p.world.Resources.Config
	// Strict bounds: area must be completely inside map
	if x < 0 || y < 0 || x+width > config.MapWidth || y+height > config.MapHeight {
		return false
	}

	// Check for any blocking walls in the area
	return !p.HasBlockingWallInAreaUnsafe(x, y, width, height, mask)
}

// IsOutOfBounds checks if position is outside spatial grid bounds
func (p *Position) IsOutOfBounds(x, y int) bool {
	return x < 0 || x >= p.world.Resources.Config.MapWidth || y < 0 || y >= p.world.Resources.Config.MapHeight
}

// HasLineOfSight checks if two grid points have unobstructed line of sight
// Uses Bresenham traversal, checking intermediate cells for blocking walls
// Acquires RLock once for entire traversal
func (p *Position) HasLineOfSight(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.HasLineOfSightUnsafe(x0, y0, x1, y1, mask)
}

// HasLineOfSightUnsafe performs Bresenham LOS without acquiring lock
// Caller MUST hold RLock() or Lock()
func (p *Position) HasLineOfSightUnsafe(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	dx := x1 - x0
	dy := y1 - y0
	absDx, absDy := dx, dy
	if absDx < 0 {
		absDx = -absDx
	}
	if absDy < 0 {
		absDy = -absDy
	}

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	err := absDx - absDy
	x, y := x0, y0

	for {
		if x == x1 && y == y1 {
			return true
		}

		// Check intermediate cells (skip origin)
		if (x != x0 || y != y0) && p.HasBlockingWallAtUnsafe(x, y, mask) {
			return false
		}

		e2 := 2 * err
		if e2 > -absDy {
			err -= absDy
			x += stepX
		}
		if e2 < absDx {
			err += absDx
			y += stepY
		}
	}
}

// --- Unsafe operation ---

// Lock manually acquires the write lock for bulk operations, MUST be paired with Unlock()
func (p *Position) Lock() {
	p.mu.Lock()
}

// Unlock releases the write lock manually
func (p *Position) Unlock() {
	p.mu.Unlock()
}

// GetUnsafe retrieves position without locking, caller MUST hold Lock/RLock
func (p *Position) GetUnsafe(e core.Entity) (component.PositionComponent, bool) {
	val, ok := p.components[e]
	return val, ok
}

// MoveUnsafe updates position without locking, caller MUST hold Lock()
func (p *Position) MoveUnsafe(e core.Entity, newPos component.PositionComponent) {
	oldPos, exists := p.components[e]
	if !exists {
		return
	}
	p.grid.RemoveEntityAt(e, oldPos.X, oldPos.Y)
	p.components[e] = newPos
	// Explicit ignore for OOB and Cell full
	_ = p.grid.Set(e, newPos.X, newPos.Y)
}

// GetAllAtIntoUnsafe copies entities at (x,y) into buf without locking, caller MUST hold Lock/RLock, returns number of entities copied
func (p *Position) GetAllAtIntoUnsafe(x, y int, buf []core.Entity) int {
	if x < 0 || x >= p.grid.Width || y < 0 || y >= p.grid.Height {
		return 0
	}

	// Direct grid access is safe because we hold the lock
	idx := y*p.grid.Width + x
	cell := &p.grid.Cells[idx]
	count := int(cell.Count)

	if count == 0 {
		return 0
	}

	if count > len(buf) {
		count = len(buf)
	}

	copy(buf, cell.Entities[:count])
	return count
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

func (p *Position) BeginBatch() *PositionBatch {
	return &PositionBatch{
		store:     p,
		additions: make([]positionAddition, 0),
	}
}

func (pb *PositionBatch) Add(e core.Entity, pos component.PositionComponent) {
	pb.additions = append(pb.additions, positionAddition{entity: e, pos: pos})
}

// Commit applies all batched additions
// Checks with HasAnyEntityAt only to prevent unintended spawns on existing entities
func (pb *PositionBatch) Commit() error {
	if pb.committed {
		return fmt.Errorf("batch already committed")
	}
	pb.committed = true

	pb.store.mu.Lock()
	defer pb.store.mu.Unlock()

	// 1. Validation phase (Gameplay logic: don't spawn on top of things)
	// Check both the current grid AND the pending batch for conflicts
	batchOccupied := make(map[int]map[int]bool)

	for _, add := range pb.additions {
		// Check against existing entities
		if pb.store.grid.HasAnyEntityAt(add.pos.X, add.pos.Y) {
			// Collision found in world
			return fmt.Errorf("position is occupied")
		}

		// Check against other items in this batch
		if batchOccupied[add.pos.Y] == nil {
			batchOccupied[add.pos.Y] = make(map[int]bool)
		}
		if batchOccupied[add.pos.Y][add.pos.X] {
			return fmt.Errorf("batch conflict at position")
		}
		batchOccupied[add.pos.Y][add.pos.X] = true
	}

	// 2. Application phase
	for _, add := range pb.additions {
		// RemoveEntityAt old position if exists
		if oldPos, exists := pb.store.components[add.entity]; exists {
			pb.store.grid.RemoveEntityAt(add.entity, oldPos.X, oldPos.Y)
		} else {
			pb.store.entities = append(pb.store.entities, add.entity)
		}

		pb.store.components[add.entity] = add.pos
		// Explicit ignore for OOB and Cell full
		_ = pb.store.grid.Set(add.entity, add.pos.X, add.pos.Y)
	}

	return nil
}

// CommitForce applies batch addition without checking for existing entity collisions
// Used for effects like Dust that overlay existing entities or replace them before death processing
func (pb *PositionBatch) CommitForce() {
	if pb.committed {
		return
	}
	pb.committed = true

	pb.store.mu.Lock()
	defer pb.store.mu.Unlock()

	for _, add := range pb.additions {
		// RemoveEntityAt old position if exists
		if oldPos, exists := pb.store.components[add.entity]; exists {
			pb.store.grid.RemoveEntityAt(add.entity, oldPos.X, oldPos.Y)
		} else {
			pb.store.entities = append(pb.store.entities, add.entity)
		}

		pb.store.components[add.entity] = add.pos
		// Explicit ignore for OOB and Cell full
		_ = pb.store.grid.Set(add.entity, add.pos.X, add.pos.Y)
	}
}

// GridStats returns computed statistics for the spatial grid
func (p *Position) GridStats() GridStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.grid.ComputeStats()
}

// GridDimensions returns width and height of the spatial grid
func (p *Position) GridDimensions() (width, height int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.grid.Width, p.grid.Height
}

// RemoveBatch deletes multiple entities in a single pass
func (p *Position) RemoveBatch(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Build removal set, remove from grid and map
	toRemove := make(map[core.Entity]struct{}, len(entities))
	for _, e := range entities {
		if pos, exists := p.components[e]; exists {
			toRemove[e] = struct{}{}
			p.grid.RemoveEntityAt(e, pos.X, pos.Y)
			delete(p.components, e)
		}
	}

	if len(toRemove) == 0 {
		return
	}

	// Single pass compaction
	writeIdx := 0
	for _, e := range p.entities {
		if _, remove := toRemove[e]; !remove {
			p.entities[writeIdx] = e
			writeIdx++
		}
	}
	p.entities = p.entities[:writeIdx]
}

// --- Range Operations ---

// ScanLineResult holds entities found during line scan
type ScanLineResult struct {
	Entity core.Entity
	X, Y   int
}

// ScanLine traverses cells from (startX, startY) in direction (dx, dy) until bounds or maxSteps
// Acquires lock once for entire scan. Returns slice of (entity, x, y) for cells with matching filter
// filter: nil = all entities, or func to test entity (e.g. HasGlyph)
func (p *Position) ScanLine(startX, startY, dx, dy, maxSteps int, filter func(core.Entity) bool) []ScanLineResult {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var results []ScanLineResult
	x, y := startX, startY

	for step := 0; step < maxSteps; step++ {
		if x < 0 || x >= p.grid.Width || y < 0 || y >= p.grid.Height {
			break
		}

		idx := y*p.grid.Width + x
		cell := &p.grid.Cells[idx]

		for i := uint8(0); i < cell.Count; i++ {
			e := cell.Entities[i]
			if filter == nil || filter(e) {
				results = append(results, ScanLineResult{Entity: e, X: x, Y: y})
			}
		}

		x += dx
		y += dy
	}

	return results
}

// ScanLineFirst returns first entity matching filter along line, or (0, -1, -1) if none
// Single-lock scan optimized for finding first match
func (p *Position) ScanLineFirst(startX, startY, dx, dy, maxSteps int, filter func(core.Entity) bool) (core.Entity, int, int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	x, y := startX, startY

	for step := 0; step < maxSteps; step++ {
		if x < 0 || x >= p.grid.Width || y < 0 || y >= p.grid.Height {
			break
		}

		idx := y*p.grid.Width + x
		cell := &p.grid.Cells[idx]

		for i := uint8(0); i < cell.Count; i++ {
			e := cell.Entities[i]
			if filter == nil || filter(e) {
				return e, x, y
			}
		}

		x += dx
		y += dy
	}

	return 0, -1, -1
}

// FindClosestEntityInDirection searches for entities in a cardinal direction (up, down, left, right)
// within the specified bounds. It enforces "Center-Oriented Consolidation".
// Returns (entity, x, y, found).
func (p *Position) FindClosestEntityInDirection(startX, startY, dx, dy int, bounds PingAbsoluteBounds, filter func(core.Entity) bool) (core.Entity, int, int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Direction handling
	if dy != 0 {
		// VERTICAL SCAN (Up/Down)
		stepY := 1
		if dy < 0 {
			stepY = -1
		}

		// Main Axis (Y) always extends to grid edge
		// Cross Axis (X) is constrained by bounds in inner loop
		limitMinY, limitMaxY := 0, p.grid.Height-1

		// Loop Y from start+step
		y := startY + stepY
		for {
			// Check Main Axis bounds
			if stepY > 0 {
				if y > limitMaxY {
					break
				}
			} else {
				if y < limitMinY {
					break
				}
			}

			// Safety grid bounds (redundant but safe)
			if y < 0 || y >= p.grid.Height {
				break
			}

			// Scan the row segment [MinX, MaxX] (Cross Axis)
			bestEntity := core.Entity(0)
			bestX := -1
			minDist := math.MaxInt

			// Iterate X in bounds
			// In Normal Mode, MinX==MaxX==startX, so we scan 1 cell (column mode).
			// In Visual Mode, we scan the full radius width.
			for x := bounds.MinX; x <= bounds.MaxX; x++ {
				if x < 0 || x >= p.grid.Width {
					continue
				}

				// Check cell
				idx := y*p.grid.Width + x
				cell := &p.grid.Cells[idx]
				if cell.Count == 0 {
					continue
				}

				// Check entities in cell
				for i := uint8(0); i < cell.Count; i++ {
					e := cell.Entities[i]
					if filter == nil || filter(e) {
						// Found a candidate. Is it closer to center (startX)?
						dist := vmath.IntAbs(x - startX)
						if dist < minDist {
							minDist = dist
							bestEntity = e
							bestX = x
						}
					}
				}
			}

			// If we found anything in this row, return the best one (consolidation)
			// We return the *first* row encountered (closest to cursor Y)
			if bestEntity != 0 {
				return bestEntity, bestX, y, true
			}

			y += stepY
		}

	} else if dx != 0 {
		// HORIZONTAL SCAN (Left/Right)
		stepX := 1
		if dx < 0 {
			stepX = -1
		}

		// Main Axis (X) always extends to grid edge
		// Cross Axis (Y) is constrained by bounds in inner loop
		limitMinX, limitMaxX := 0, p.grid.Width-1

		x := startX + stepX
		for {
			if stepX > 0 {
				if x > limitMaxX {
					break
				}
			} else {
				if x < limitMinX {
					break
				}
			}

			if x < 0 || x >= p.grid.Width {
				break
			}

			bestEntity := core.Entity(0)
			bestY := -1
			minDist := math.MaxInt

			// Scan the col segment [MinY, MaxY] (Cross Axis)
			for y := bounds.MinY; y <= bounds.MaxY; y++ {
				if y < 0 || y >= p.grid.Height {
					continue
				}

				idx := y*p.grid.Width + x
				cell := &p.grid.Cells[idx]
				if cell.Count == 0 {
					continue
				}

				for i := uint8(0); i < cell.Count; i++ {
					e := cell.Entities[i]
					if filter == nil || filter(e) {
						dist := vmath.IntAbs(y - startY)
						if dist < minDist {
							minDist = dist
							bestEntity = e
							bestY = y
						}
					}
				}
			}

			if bestEntity != 0 {
				return bestEntity, x, bestY, true
			}

			x += stepX
		}
	}

	return 0, -1, -1, false
}

// --- Spiral search: Game area, not Spatial Grid ---

// PatternType defines search pattern for FindFreeFromPattern
type PatternType uint8

const (
	// PatternCardinalFirst searches cardinals (N,S,E,W) then diagonals
	PatternCardinalFirst PatternType = iota
	// PatternDiagonalFirst searches diagonals then cardinals
	PatternDiagonalFirst
)

// SearchDirection defines pattern rotation direction
type SearchDirection uint8

const (
	SearchCW SearchDirection = iota
	SearchCCW
)

// Pre-computed 8-direction offsets (unit vectors)
// Index 0 = Top (N), proceeding clockwise
var patternDirections = [8][2]int{
	{0, -1},  // 0: N
	{1, -1},  // 1: NE
	{1, 0},   // 2: E
	{1, 1},   // 3: SE
	{0, 1},   // 4: S
	{-1, 1},  // 5: SW
	{-1, 0},  // 6: W
	{-1, -1}, // 7: NW
}

// var cardinalFirstCW = [8]int{0, 2, 1, 3, 4, 6, 7, 5}  // Bottom→Right→Top→Left, then BR→TR→TL→BL
// var cardinalFirstCCW = [8]int{0, 3, 1, 2, 5, 7, 6, 4} // Bottom→Left→Top→Right, then BL→TL→TR→BR
// var diagonalFirstCW = [8]int{1, 3, 5, 7, 0, 2, 4, 6}
// var diagonalFirstCCW = [8]int{7, 5, 3, 1, 0, 6, 4, 2}

// Angle orders for pattern searches
// patternDirections index: 0=N, 1=NE, 2=E, 3=SE, 4=S, 5=SW, 6=W, 7=NW
// Cardinals (N,E,S,W) = indices 0,2,4,6 | Diagonals (NE,SE,SW,NW) = indices 1,3,5,7
var cardinalFirstCW = [8]int{0, 2, 4, 6, 1, 3, 5, 7}  // N→E→S→W, then NE→SE→SW→NW
var cardinalFirstCCW = [8]int{0, 6, 4, 2, 7, 5, 3, 1} // N→W→S→E, then NW→SW→SE→NE
var diagonalFirstCW = [8]int{1, 3, 5, 7, 0, 2, 4, 6}  // NE→SE→SW→NW, then N→E→S→W
var diagonalFirstCCW = [8]int{7, 5, 3, 1, 0, 6, 4, 2} // NW→SW→SE→NE, then N→W→S→E

// FindFreeFromPattern searches 8 directions at expanding radii for free area
// Returns (absX, absY, found) - absolute Top-Left position of the placed rectangle
// originX, originY: The CENTER point to search around
// aspectCorrect: apply terminal aspect ratio (1:2) to Y offsets
// additionalCheck: optional callback for extra validation (nil = skip), returns true if valid
func (p *Position) FindFreeFromPattern(
	originX, originY int,
	width, height int,
	pattern PatternType,
	startRadius, maxRadius int,
	aspectCorrect bool,
	mask component.WallBlockMask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Compute direction internally
	centerX := p.world.Resources.Config.MapWidth / 2
	direction := getSearchDirection(originX, centerX)

	var order [8]int
	switch {
	case pattern == PatternCardinalFirst && direction == SearchCW:
		order = cardinalFirstCW
	case pattern == PatternCardinalFirst && direction == SearchCCW:
		order = cardinalFirstCCW
	case pattern == PatternDiagonalFirst && direction == SearchCW:
		order = diagonalFirstCW
	default:
		order = diagonalFirstCCW
	}

	for radius := startRadius; radius <= maxRadius; radius++ {
		for _, idx := range order {
			dir := patternDirections[idx]

			// Integer Circular Approximation (No Floats)
			// Scale diagonals by 7/10 to approximate 0.707 (1/sqrt(2))
			r := radius
			if dir[0] != 0 && dir[1] != 0 {
				r = (radius * 7) / 10
			}

			offsetX := dir[0] * r
			offsetY := dir[1] * r

			if aspectCorrect {
				// Y-axis aspect correction (1/2 scaling for visual circularity)
				offsetY = offsetY / 2
			}

			// Center the object on the search point
			// absX is the Top-Left coordinate of the candidate rectangle
			absX := originX + offsetX - (width / 2)
			absY := originY + offsetY - (height / 2)

			// Strict Bounds Check (OOB)
			// absX must be >= 0 and absX + width must be <= Width (Strict inclusion)
			if absX < 0 || absX+width > p.world.Resources.Config.MapWidth ||
				absY < 0 || absY+height > p.world.Resources.Config.MapHeight {
				continue
			}

			// Wall/Grid Collision Check
			if !p.isAreaFreeUnsafe(absX, absY, width, height, mask) {
				continue
			}

			// External Entity Collision Check
			if additionalCheck != nil && !additionalCheck(absX, absY, width, height) {
				continue
			}

			return absX, absY, true
		}
	}

	return 0, 0, false
}

// Exclusion defines a rectangular keep-out zone relative to an anchor point
type Exclusion struct {
	Left, Right, Top, Bottom int
}

// Order relative to anchor
// offsets array: 0=Bottom, 1=Top, 2=Right, 3=Left, 4=BR, 5=BL, 6=TR, 7=TL
var (
	anchorRelativeCardinalFirstCW  = [8]int{0, 2, 1, 3, 4, 6, 7, 5} // Bottom→Right→Top→Left, then BR→TR→TL→BL
	anchorRelativeCardinalFirstCCW = [8]int{0, 3, 1, 2, 5, 7, 6, 4} // Bottom→Left→Top→Right, then BL→TL→TR→BR
	anchorRelativeDiagonalFirstCW  = [8]int{4, 6, 7, 5, 0, 2, 1, 3} // BR→TR→TL→BL, then Bottom→Right→Top→Left
	anchorRelativeDiagonalFirstCCW = [8]int{5, 7, 6, 4, 0, 3, 1, 2} // BL→TL→TR→BR, then
)

// FindPlacementAroundExclusion finds valid position for object outside exclusion zone
// Returns (offsetX, offsetY, found) where offset is relative to anchor
// padding: gap between exclusion edge and placed object
// topAdjust: visual compensation for font asymmetry (typically -1)
func (p *Position) FindPlacementAroundExclusion(
	anchorX, anchorY int,
	objectW, objectH int,
	exclusion Exclusion,
	padding, topAdjust int,
	pattern PatternType,
	mask component.WallBlockMask,
	additionalCheck func(absX, absY, w, h int) bool,
) (int, int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := p.world.Resources.Config
	centerX := config.MapWidth / 2
	direction := getSearchDirection(anchorX, centerX)

	// Compute centering offsets
	objHalfW := objectW / 2
	objHalfH := objectH / 2
	hCenter := (exclusion.Right - exclusion.Left) / 2

	// 8 positions: cardinals (0-3), diagonals (4-7)
	// Matches timer positioning logic exactly
	offsets := [8][2]int{
		{hCenter - objHalfW, exclusion.Bottom + padding},                                      // 0: Bottom
		{hCenter - objHalfW, -exclusion.Top - objectH - padding + topAdjust},                  // 1: Top
		{exclusion.Right + padding, -objHalfH + topAdjust},                                    // 2: Right
		{-exclusion.Left - objectW - padding, -objHalfH + topAdjust},                          // 3: Left
		{exclusion.Right + padding, exclusion.Bottom + padding},                               // 4: Bottom-right
		{-exclusion.Left - objectW - padding, exclusion.Bottom + padding},                     // 5: Bottom-left
		{exclusion.Right + padding, -exclusion.Top - objectH + padding + topAdjust},           // 6: Top-right
		{-exclusion.Left - objectW - padding, -exclusion.Top - objectH + padding + topAdjust}, // 7: Top-left
	}

	var order [8]int
	switch {
	case pattern == PatternCardinalFirst && direction == SearchCW:
		order = anchorRelativeCardinalFirstCW
	case pattern == PatternCardinalFirst && direction == SearchCCW:
		order = anchorRelativeCardinalFirstCCW
	case pattern == PatternDiagonalFirst && direction == SearchCW:
		order = anchorRelativeDiagonalFirstCW
	default:
		order = anchorRelativeDiagonalFirstCCW
	}

	// Primary: all 8 positions
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		if absX < 0 || absX+objectW > p.world.Resources.Config.MapWidth ||
			absY < 0 || absY+objectH > p.world.Resources.Config.MapHeight {
			continue
		}

		if additionalCheck != nil && !additionalCheck(absX, absY, objectW, objectH) {
			continue
		}

		if !p.isAreaFreeUnsafe(absX, absY, objectW, objectH, mask) {
			continue
		}

		return offsets[idx][0], offsets[idx][1], true
	}

	// Secondary: 2x distance
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]*2
		absY := anchorY + offsets[idx][1]*2

		if absX < 0 || absX+objectW > config.MapWidth ||
			absY < 0 || absY+objectH > config.MapHeight {
			continue
		}

		if additionalCheck != nil && !additionalCheck(absX, absY, objectW, objectH) {
			continue
		}

		if !p.isAreaFreeUnsafe(absX, absY, objectW, objectH, mask) {
			continue
		}

		return offsets[idx][0] * 2, offsets[idx][1] * 2, true
	}

	// Tertiary: skip additionalCheck, keep wall avoidance
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		if absX < 0 || absX+objectW > config.MapWidth ||
			absY < 0 || absY+objectH > config.MapHeight {
			continue
		}

		if p.isAreaFreeUnsafe(absX, absY, objectW, objectH, mask) {
			return offsets[idx][0], offsets[idx][1], true
		}
	}

	// Quaternary: clamp to bounds, skip walls
	for _, idx := range order {
		absX := anchorX + offsets[idx][0]
		absY := anchorY + offsets[idx][1]

		absX = max(0, min(absX, config.MapWidth-objectW))
		absY = max(0, min(absY, config.MapHeight-objectH))

		if p.HasBlockingWallInAreaUnsafe(absX, absY, objectW, objectH, mask) {
			continue
		}

		return absX - anchorX, absY - anchorY, true
	}

	// Ultimate: force clamp first position
	absX := anchorX + offsets[order[0]][0]
	absY := anchorY + offsets[order[0]][1]
	absX = max(0, min(absX, config.MapWidth-objectW))
	absY = max(0, min(absY, config.MapHeight-objectH))

	return absX - anchorX, absY - anchorY, false
}

// GetSearchDirection returns CCW if origin is right of center, CW otherwise
func getSearchDirection(originX, centerX int) SearchDirection {
	if originX >= centerX {
		return SearchCCW
	}
	return SearchCW
}

// FindLastFreeOnRay returns the last unblocked cell on a ray from (startX, startY) toward (endX, endY)
// Useful for finding safe position before wall collision
// Returns (x, y, reachedEnd) where reachedEnd=true if entire path is free
func (p *Position) FindLastFreeOnRay(startX, startY, endX, endY int, mask component.WallBlockMask) (int, int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Start must be free (caller's responsibility to ensure valid origin)
	lastFreeX, lastFreeY := startX, startY

	dx := endX - startX
	dy := endY - startY
	absDx, absDy := vmath.IntAbs(dx), vmath.IntAbs(dy)

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	err := absDx - absDy
	x, y := startX, startY

	for {
		// Check current cell (skip origin)
		if (x != startX || y != startY) && p.HasBlockingWallAtUnsafe(x, y, mask) {
			return lastFreeX, lastFreeY, false
		}

		// Update last free position
		lastFreeX, lastFreeY = x, y

		if x == endX && y == endY {
			return lastFreeX, lastFreeY, true
		}

		e2 := 2 * err
		if e2 > -absDy {
			err -= absDy
			x += stepX
		}
		if e2 < absDx {
			err += absDx
			y += stepY
		}
	}
}

// IsPathBlocked checks if straight line from (x0,y0) to (x1,y1) intersects any blocking wall
// Uses Bresenham traversal, returns true if ANY intermediate cell is blocked
// Endpoints are NOT checked - only path between them
func (p *Position) IsPathBlocked(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if x0 == x1 && y0 == y1 {
		return false
	}

	dx := x1 - x0
	dy := y1 - y0
	absDx, absDy := dx, dy
	if absDx < 0 {
		absDx = -absDx
	}
	if absDy < 0 {
		absDy = -absDy
	}

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	err := absDx - absDy
	x, y := x0, y0

	for {
		e2 := 2 * err
		if e2 > -absDy {
			err -= absDy
			x += stepX
		}
		if e2 < absDx {
			err += absDx
			y += stepY
		}

		// Reached destination - path clear
		if x == x1 && y == y1 {
			return false
		}

		// Check intermediate cell
		if p.HasBlockingWallAtUnsafe(x, y, mask) {
			return true
		}
	}
}

// IsPointValidForOrbit checks if grid point is within bounds and not wall-blocked
func (p *Position) IsPointValidForOrbit(x, y int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := p.world.Resources.Config
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}
	return !p.HasBlockingWallAtUnsafe(x, y, mask)
}

// HasAreaLineOfSight checks if rectangular entity can traverse unobstructed from (x0,y0) to (x1,y1)
// Entity bounding box (width×height) is centered at each intermediate path cell
// Returns false if any intermediate position causes wall collision or exits map bounds
// For 1×1 entities, delegates to point-based HasLineOfSight
func (p *Position) HasAreaLineOfSight(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.HasAreaLineOfSightUnsafe(x0, y0, x1, y1, width, height, mask)
}

// HasAreaLineOfSightUnsafe performs area LOS check without acquiring lock
// Caller MUST hold RLock() or Lock()
func (p *Position) HasAreaLineOfSightUnsafe(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	// Degenerate case: point entity
	if width <= 1 && height <= 1 {
		return p.HasLineOfSightUnsafe(x0, y0, x1, y1, mask)
	}

	config := p.world.Resources.Config
	halfW := width / 2
	halfH := height / 2

	dx := x1 - x0
	dy := y1 - y0
	absDx, absDy := dx, dy
	if absDx < 0 {
		absDx = -absDx
	}
	if absDy < 0 {
		absDy = -absDy
	}

	stepX, stepY := 1, 1
	if dx < 0 {
		stepX = -1
	}
	if dy < 0 {
		stepY = -1
	}

	err := absDx - absDy
	x, y := x0, y0

	for {
		if x == x1 && y == y1 {
			return true
		}

		// Skip origin, check intermediate cells
		if x != x0 || y != y0 {
			boxX := x - halfW
			boxY := y - halfH

			// Bounds check: entity bbox must fit entirely within map
			if boxX < 0 || boxY < 0 || boxX+width > config.MapWidth || boxY+height > config.MapHeight {
				return false
			}

			// Wall collision
			if p.HasBlockingWallInAreaUnsafe(boxX, boxY, width, height, mask) {
				return false
			}
		}

		e2 := 2 * err
		if e2 > -absDy {
			err -= absDy
			x += stepX
		}
		if e2 < absDx {
			err += absDx
			y += stepY
		}
	}
}

// HasAreaLineOfSightRotatable checks LOS with optional 90° rotation
// Tries width×height first, then height×width if blocked
// Heuristic for elongated entities that may rotate to fit through corridors
func (p *Position) HasAreaLineOfSightRotatable(x0, y0, x1, y1, width, height int, mask component.WallBlockMask) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.HasAreaLineOfSightUnsafe(x0, y0, x1, y1, width, height, mask) {
		return true
	}

	// Square entities cannot benefit from rotation
	if width == height {
		return false
	}

	return p.HasAreaLineOfSightUnsafe(x0, y0, x1, y1, height, width, mask)
}