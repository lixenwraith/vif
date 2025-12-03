package core

// Point represents a 2D coordinate
type Point struct {
	X, Y int
}

// Style represents cell styling for spatial buffer
// Minimal type for entity tracking; actual rendering uses render.RenderBuffer
// TODO: do we even need this?
type Style struct{}

// StyleDefault is the zero-value placeholder
var StyleDefault = Style{}

// Cell represents a single cell in the buffer
type Cell struct {
	Rune   rune
	Style  Style
	Entity uint64 // Optional entity at this position (0 if none)
}

// Buffer represents a 2D grid of cells with spatial indexing
type Buffer struct {
	width   int
	height  int
	lines   [][]Cell         // 2D grid of cells
	dirty   map[Point]bool   // Changed cells for efficient rendering
	spatial map[Point]uint64 // Spatial index: position -> entity ID
}

// NewBuffer creates a new buffer with the given dimensions
func NewBuffer(width, height int) *Buffer {
	lines := make([][]Cell, height)
	for y := 0; y < height; y++ {
		lines[y] = make([]Cell, width)
		for x := 0; x < width; x++ {
			lines[y][x] = Cell{
				Rune:   ' ',
				Style:  StyleDefault,
				Entity: 0,
			}
		}
	}

	return &Buffer{
		width:   width,
		height:  height,
		lines:   lines,
		dirty:   make(map[Point]bool),
		spatial: make(map[Point]uint64),
	}
}

// Width returns the buffer width
func (b *Buffer) Width() int {
	return b.width
}

// Height returns the buffer height
func (b *Buffer) Height() int {
	return b.height
}

// Resize resizes the buffer, preserving existing content where possible
func (b *Buffer) Resize(newWidth, newHeight int) {
	newLines := make([][]Cell, newHeight)
	for y := 0; y < newHeight; y++ {
		newLines[y] = make([]Cell, newWidth)
		for x := 0; x < newWidth; x++ {
			if y < b.height && x < b.width {
				// Copy existing cell
				newLines[y][x] = b.lines[y][x]
			} else {
				// Initialize new cell
				newLines[y][x] = Cell{
					Rune:   ' ',
					Style:  StyleDefault,
					Entity: 0,
				}
			}
		}
	}

	// Clean up spatial index for out-of-bounds positions
	newSpatial := make(map[Point]uint64)
	for p, entity := range b.spatial {
		if p.X < newWidth && p.Y < newHeight {
			newSpatial[p] = entity
		}
	}

	b.width = newWidth
	b.height = newHeight
	b.lines = newLines
	b.spatial = newSpatial
	b.dirty = make(map[Point]bool) // Mark all as dirty after resize
}

// GetCell returns the cell at the given position
func (b *Buffer) GetCell(x, y int) (Cell, bool) {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return Cell{}, false
	}
	return b.lines[y][x], true
}

// SetCell sets the cell at the given position and marks it as dirty
func (b *Buffer) SetCell(x, y int, cell Cell) bool {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return false
	}

	b.lines[y][x] = cell
	b.dirty[Point{X: x, Y: y}] = true

	// Update spatial index
	p := Point{X: x, Y: y}
	if cell.Entity != 0 {
		b.spatial[p] = cell.Entity
	} else {
		delete(b.spatial, p)
	}

	return true
}

// SetContent sets the content at the given position
func (b *Buffer) SetContent(x, y int, r rune, style Style, entity uint64) bool {
	return b.SetCell(x, y, Cell{
		Rune:   r,
		Style:  style,
		Entity: entity,
	})
}

// Clear clears the entire buffer
func (b *Buffer) Clear(style Style) {
	for y := 0; y < b.height; y++ {
		for x := 0; x < b.width; x++ {
			b.lines[y][x] = Cell{
				Rune:   ' ',
				Style:  style,
				Entity: 0,
			}
		}
	}
	b.spatial = make(map[Point]uint64)
	b.dirty = make(map[Point]bool)
}

// GetEntityAt returns the entity ID at the given position (0 if none)
// This provides O(1) lookups via spatial indexing
func (b *Buffer) GetEntityAt(x, y int) uint64 {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return 0
	}
	return b.spatial[Point{X: x, Y: y}]
}

// HasEntityAt returns true if there's an entity at the given position
func (b *Buffer) HasEntityAt(x, y int) bool {
	return b.GetEntityAt(x, y) != 0
}

// DirtyRegions returns all dirty positions
func (b *Buffer) DirtyRegions() []Point {
	regions := make([]Point, 0, len(b.dirty))
	for p := range b.dirty {
		regions = append(regions, p)
	}
	return regions
}

// ClearDirty clears all dirty flags
func (b *Buffer) ClearDirty() {
	b.dirty = make(map[Point]bool)
}

// MarkDirty marks a position as dirty
func (b *Buffer) MarkDirty(x, y int) {
	if x >= 0 && x < b.width && y >= 0 && y < b.height {
		b.dirty[Point{X: x, Y: y}] = true
	}
}

// GetLine returns all cells in a given row
func (b *Buffer) GetLine(y int) []Cell {
	if y < 0 || y >= b.height {
		return nil
	}
	// Return a copy to prevent external modification
	line := make([]Cell, b.width)
	copy(line, b.lines[y])
	return line
}