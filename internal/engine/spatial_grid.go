package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// DomainScope selects which half of a partitioned cell an enumeration visits.
type DomainScope uint8

const (
	ScopeShared DomainScope = 1 << iota
	ScopePlayer
)

// ScopeBoth enumerates the whole cell; renderers and lifecycle paths use it.
const ScopeBoth = ScopeShared | ScopePlayer

// Selects reports whether a scope includes an entity's domain.
func (s DomainScope) Selects(e core.Entity) bool {
	if e.Domain() == core.DomainPlayer {
		return s&ScopePlayer != 0
	}
	return s&ScopeShared != 0
}

// Cell represents a single grid cell containing a fixed number of entities.
// Invariant: shared occupy Entities[:SharedCount], player occupy Entities[SharedCount:Count].
type Cell struct {
	Count       uint8
	SharedCount uint8
	_           [6]byte // Explicit padding to ensure 8-byte alignment for Entities
	Entities    [parameter.MaxEntitiesPerCell]core.Entity
}

// view returns the sub-slice a scope selects; the partition makes every scope one contiguous range.
func (c *Cell) view(scope DomainScope) []core.Entity {
	switch scope & ScopeBoth {
	case ScopeShared:
		return c.Entities[:c.SharedCount]
	case ScopePlayer:
		return c.Entities[c.SharedCount:c.Count]
	case ScopeBoth:
		return c.Entities[:c.Count]
	}
	return nil
}

// SpatialGrid is a dense 2D grid for fast spatial queries without allocation
type SpatialGrid struct {
	Cells  []Cell // 1D array: index = y*Width + x
	Width  int
	Height int
}

// NewSpatialGrid creates a new grid with the specified dimensions
func NewSpatialGrid(width, height int) *SpatialGrid {
	return &SpatialGrid{
		Cells:  make([]Cell, width*height),
		Width:  width,
		Height: height,
	}
}

// Set inserts an entity into its domain partition at (x, y).
// O(1), returns false if bounds invalid, cell full, or the player budget is spent (soft clip).
func (g *SpatialGrid) Set(e core.Entity, x, y int) bool {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return false
	}

	cell := &g.Cells[y*g.Width+x]
	if cell.Count >= parameter.MaxEntitiesPerCell {
		return false
	}

	// Player insertion appends inside its own budget and never touches the shared run
	if e.Domain() == core.DomainPlayer {
		if cell.Count-cell.SharedCount >= parameter.ReservedPlayerPerCell {
			return false
		}
		cell.Entities[cell.Count] = e
		cell.Count++
		return true
	}

	// Shared insertion splits the run: the first player entity moves to the tail
	cell.Entities[cell.Count] = cell.Entities[cell.SharedCount]
	cell.Entities[cell.SharedCount] = e
	cell.SharedCount++
	cell.Count++
	return true
}

// RemoveEntityAt deletes an entity from the grid at (x, y).
// O(k) where k <= 31. Swap-removes within the domain partition, refilling the shared tail from the player run.
func (g *SpatialGrid) RemoveEntityAt(e core.Entity, x, y int) {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return
	}

	cell := &g.Cells[y*g.Width+x]
	for i := uint8(0); i < cell.Count; i++ {
		if cell.Entities[i] != e {
			continue
		}

		if i < cell.SharedCount {
			// Close the shared run, then pull the last player into the vacated shared tail
			lastShared := cell.SharedCount - 1
			cell.Entities[i] = cell.Entities[lastShared]
			cell.Count--
			cell.SharedCount--
			if lastShared != cell.Count {
				cell.Entities[lastShared] = cell.Entities[cell.Count]
			}
		} else {
			cell.Count--
			if i != cell.Count {
				cell.Entities[i] = cell.Entities[cell.Count]
			}
		}

		cell.Entities[cell.Count] = 0
		return
	}
}

// EntitiesAt returns a slice view of the entities a scope selects at (x, y)
// INTERNAL USE ONLY - callers must copy or hold external lock
// O(1), returns nil if empty or out of bounds
func (g *SpatialGrid) EntitiesAt(x, y int, scope DomainScope) []core.Entity {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return nil
	}
	return g.Cells[y*g.Width+x].view(scope)
}

// HasAnyEntityAt returns true if the scope selects at least one entity at (x, y). O(1)
func (g *SpatialGrid) HasAnyEntityAt(x, y int, scope DomainScope) bool {
	return len(g.EntitiesAt(x, y, scope)) > 0
}

// HasAnyEntityInArea checks if any in-scope entity within the rectangular area satisfies the predicate
// It iterates only the intersection of the requested area and the grid bounds
// Returns true immediately if the predicate returns true for any entity
func (g *SpatialGrid) HasAnyEntityInArea(x, y, width, height int, scope DomainScope, predicate func(core.Entity) bool) bool {
	// Clamp query area to grid dimensions to avoid OOB
	startX := max(0, x)
	startY := max(0, y)
	endX := min(g.Width, x+width)
	endY := min(g.Height, y+height)

	if startX >= endX || startY >= endY {
		return false
	}

	for row := startY; row < endY; row++ {
		// Calculate row offset once
		rowOffset := row * g.Width
		for col := startX; col < endX; col++ {
			cell := &g.Cells[rowOffset+col]
			for _, e := range cell.view(scope) {
				if predicate(e) {
					return true
				}
			}
		}
	}
	return false
}

// Clear removes all entities from all cells
func (g *SpatialGrid) Clear() {
	for i := range g.Cells {
		g.Cells[i].Count = 0
		g.Cells[i].SharedCount = 0
	}
}

// Resize reconfigures logical grid dimensions. Backing storage is grow-only:
// shrinking retains capacity, so map-size oscillation (tmux pane resize,
// crop-on-resize, wasm) never reallocates; growth allocates once and holds.
// Contents are cleared; Position.ResizeGrid repopulates from component data.
func (g *SpatialGrid) Resize(newWidth, newHeight int) {
	need := newWidth * newHeight
	if need <= cap(g.Cells) {
		g.Cells = g.Cells[:need]
		// Must clear the full range: re-grown cells may hold stale counts
		// from a previous larger layout
		for i := range g.Cells {
			g.Cells[i].Count = 0
			g.Cells[i].SharedCount = 0
		}
	} else {
		g.Cells = make([]Cell, need)
	}
	g.Width = newWidth
	g.Height = newHeight
}

// GridStats holds computed statistics for the spatial grid
type GridStats struct {
	CellsOccupied  int
	EntitiesTotal  int
	EntitiesShared int
	MaxOccupancy   int
}

// ComputeStats calculates grid statistics in a single pass
// O(n) where n = Width * Height
func (g *SpatialGrid) ComputeStats() GridStats {
	var stats GridStats
	for i := range g.Cells {
		count := int(g.Cells[i].Count)
		if count > 0 {
			stats.CellsOccupied++
			stats.EntitiesTotal += count
			stats.EntitiesShared += int(g.Cells[i].SharedCount)
			if count > stats.MaxOccupancy {
				stats.MaxOccupancy = count
			}
		}
	}
	return stats
}
