package engine

// MaxEntitiesPerCell is set to 15 to ensure the Cell struct fits exactly into 128 bytes
// (2 cache lines) when Entity is uint64 (8 bytes)
// 15 * 8 (Entities) + 1 (Count) + 7 (Padding) = 128 bytes
const MaxEntitiesPerCell = 15

// Cell represents a single grid cell containing a fixed number of entities
// It is a value type designed for contiguous memory layout
type Cell struct {
	Count    uint8
	_        [7]byte // Explicit padding to ensure 8-byte alignment for Entities
	Entities [MaxEntitiesPerCell]Entity
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

// Add inserts an entity into the grid at (x, y)
// O(1), No-op if bounds are invalid or cell is full (soft clip)
func (g *SpatialGrid) Add(e Entity, x, y int) {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return
	}

	idx := y*g.Width + x
	cell := &g.Cells[idx] // Get pointer to avoid copy

	if cell.Count < MaxEntitiesPerCell {
		cell.Entities[cell.Count] = e
		cell.Count++
	}
	// Soft clip: if cell is full (15 entities), ignores 16th+
	// Prevents allocation spikes during extreme overlaps
}

// Remove deletes an entity from the grid at (x, y)
// O(k) where k <= 15. Uses swap-remove to maintain dense packing
func (g *SpatialGrid) Remove(e Entity, x, y int) {
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

// Move atomically updates an entity's position in the grid
func (g *SpatialGrid) Move(e Entity, oldX, oldY, newX, newY int) {
	g.Remove(e, oldX, oldY)
	g.Add(e, newX, newY)
}

// GetAllAt returns a slice view of entities at (x, y)
// INTERNAL USE ONLY - callers must copy or hold external lock
// O(1), returns nil if empty or out of bounds
func (g *SpatialGrid) GetAllAt(x, y int) []Entity {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return nil
	}

	cell := &g.Cells[y*g.Width+x]
	if cell.Count == 0 {
		return nil
	}

	return cell.Entities[:cell.Count]
}

// HasAny returns true if there is at least one entity at (x, y). O(1)
func (g *SpatialGrid) HasAny(x, y int) bool {
	if x < 0 || x >= g.Width || y < 0 || y >= g.Height {
		return false
	}
	return g.Cells[y*g.Width+x].Count > 0
}

// Clear resets all cells in the grid
func (g *SpatialGrid) Clear() {
	// Zeroing the entire slice is faster than iterating rows for fixed size
	for i := range g.Cells {
		g.Cells[i].Count = 0
		// No need to zero Entities array, Count controls access
	}
}

// Resize resizes the grid, clearing all data
// This does NOT preserve entities because re-mapping them from components
// is the responsibility of the PositionStore
func (g *SpatialGrid) Resize(newWidth, newHeight int) {
	g.Width = newWidth
	g.Height = newHeight
	g.Cells = make([]Cell, newWidth*newHeight)
}