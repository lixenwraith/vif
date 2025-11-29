package render

import "github.com/gdamore/tcell/v2"

// RenderBuffer is a dense grid for compositing render output
type RenderBuffer struct {
	cells  []RenderCell
	width  int
	height int
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(width, height int) *RenderBuffer {
	size := width * height
	cells := make([]RenderCell, size)
	for i := range cells {
		cells[i] = emptyCell
	}
	return &RenderBuffer{
		cells:  cells,
		width:  width,
		height: height,
	}
}

// Resize adjusts buffer dimensions, reallocates only if capacity insufficient
func (b *RenderBuffer) Resize(width, height int) {
	size := width * height
	if cap(b.cells) < size {
		b.cells = make([]RenderCell, size)
	} else {
		b.cells = b.cells[:size]
	}
	b.width = width
	b.height = height
	b.Clear()
}

// Clear resets all cells to empty using exponential copy
func (b *RenderBuffer) Clear() {
	if len(b.cells) == 0 {
		return
	}
	b.cells[0] = emptyCell
	for filled := 1; filled < len(b.cells); filled *= 2 {
		copy(b.cells[filled:], b.cells[:filled])
	}
}

// Set writes a rune and style at (x, y), Silent no-op on OOB
func (b *RenderBuffer) Set(x, y int, r rune, style tcell.Style) {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return
	}
	b.cells[y*b.width+x] = RenderCell{Rune: r, Style: style}
}

// SetRune writes a rune at (x, y) preserving existing style, Silent no-op on OOB
func (b *RenderBuffer) SetRune(x, y int, r rune) {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return
	}
	idx := y*b.width + x
	b.cells[idx].Rune = r
}

// SetString writes a string starting at (x, y) and returns runes written
func (b *RenderBuffer) SetString(x, y int, s string, style tcell.Style) int {
	if y < 0 || y >= b.height {
		return 0
	}
	written := 0
	for _, r := range s {
		if x >= b.width {
			break
		}
		if x >= 0 {
			b.cells[y*b.width+x] = RenderCell{Rune: r, Style: style}
			written++
		}
		x++
	}
	return written
}

// Get returns the cell at (x, y) and returns emptyCell on OOB
func (b *RenderBuffer) Get(x, y int) RenderCell {
	if x < 0 || x >= b.width || y < 0 || y >= b.height {
		return emptyCell
	}
	return b.cells[y*b.width+x]
}

// DecomposeAt returns fg, bg, attrs at (x, y) and returns defaults on OOB
func (b *RenderBuffer) DecomposeAt(x, y int) (fg, bg tcell.Color, attrs tcell.AttrMask) {
	cell := b.Get(x, y)
	return cell.Style.Decompose()
}

// Flush writes buffer contents to tcell.Screen
func (b *RenderBuffer) Flush(screen tcell.Screen) {
	for y := 0; y < b.height; y++ {
		row := y * b.width
		for x := 0; x < b.width; x++ {
			c := b.cells[row+x]
			screen.SetContent(x, y, c.Rune, nil, c.Style)
		}
	}
}

// Bounds returns buffer dimensions
func (b *RenderBuffer) Bounds() (width, height int) {
	return b.width, b.height
}