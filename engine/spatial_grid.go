package engine

import (
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// Cell represents a single grid cell containing a fixed number of entities
// It is a value type designed for contiguous memory layout
type Cell struct {
	Count    uint8
	_        [7]byte // Explicit padding to ensure 8-byte alignment for Entities
	Entities [parameter.MaxEntitiesPerCell]core.Entity
}

// SpatialGrid is a dense 2D grid for fast spatial queries without allocation
type SpatialGrid struct {
	Width  int
	Height int
	Cells  []Cell // 1D array: index = y*Width + x
}

// NewSpatialGrid creates a new grid with the specified dimensions
func NewSpatialGrid(width, height int) *SpatialGrid {
	return &SpatialGrid{
		Width:  width,
		Height: height,
		Cells:  make([]Cell, width*height),
	}
}

// AddEntityAt inserts an entity into the grid at (x, y)
// O(1), Returns false if bounds invalid or cell full (soft clip)
func (g *SpatialGrid) AddEntityAt(e core.Entity, x, y int) bool {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return false
	}

	idx := y*g.Width + x
	cell := &g.Cells[idx] // Get pointer to avoid copy

	if cell.Count < parameter.MaxEntitiesPerCell {
		cell.Entities[cell.Count] = e
		cell.Count++
		return true
	}
	return false
}

// RemoveEntityAt deletes an entity from the grid at (x, y)
// O(k) where k <= 15. Uses swap-remove to maintain dense packing
func (g *SpatialGrid) RemoveEntityAt(e core.Entity, x, y int) {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return
	}

	idx := y*g.Width + x
	cell := &g.Cells[idx]

	for i := uint8(0); i < cell.Count; i++ {
		if cell.Entities[i] == e {
			// Decrease count first to get index of last element
			cell.Count--

			// Swap with the last active element (unless we are the last one)
			if i < cell.Count {
				cell.Entities[i] = cell.Entities[cell.Count]
			}

			// Zero out the old slot to avoid holding references (though strictly optional for integers)
			cell.Entities[cell.Count] = 0
			return
		}
	}
}

// GetAllEntitiesAt returns a slice view of entities at (x, y)
// INTERNAL USE ONLY - callers must copy or hold external lock
// O(1), returns nil if empty or out of bounds
func (g *SpatialGrid) GetAllEntitiesAt(x, y int) []core.Entity {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return nil
	}

	cell := &g.Cells[y*g.Width+x]
	if cell.Count == 0 {
		return nil
	}

	return cell.Entities[:cell.Count]
}

// HasAnyEntityAt returns true if there is at least one entity at (x, y). O(1)
func (g *SpatialGrid) HasAnyEntityAt(x, y int) bool {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return false
	}
	return g.Cells[y*g.Width+x].Count > 0
}

// HasAnyEntityInArea checks if any entity within the rectangular area satisfies the predicate
// It iterates only the intersection of the requested area and the grid bounds
// Returns true immediately if the predicate returns true for any entity
func (g *SpatialGrid) HasAnyEntityInArea(x, y, width, height int, predicate func(core.Entity) bool) bool {
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
			idx := rowOffset + col
			cell := &g.Cells[idx]
			// Check entities in this cell
			for i := uint8(0); i < cell.Count; i++ {
				if predicate(cell.Entities[i]) {
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
	}
}

// Resize resizes the grid, clearing all data
// This does NOT preserve entities because re-mapping them from components
// is the responsibility of the Position
func (g *SpatialGrid) Resize(newWidth, newHeight int) {
	g.Width = newWidth
	g.Height = newHeight
	g.Cells = make([]Cell, newWidth*newHeight)
}

// GridStats holds computed statistics for the spatial grid
type GridStats struct {
	CellsOccupied int
	EntitiesTotal int
	MaxOccupancy  int
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
			if count > stats.MaxOccupancy {
				stats.MaxOccupancy = count
			}
		}
	}
	return stats
}