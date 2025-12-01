package render

import "github.com/gdamore/tcell/v2"

// RenderBuffer is a compositor backed by RGB cells
// Single source of truth - all methods write to the same backing store
type RenderBuffer struct {
	cells  []CompositorCell
	width  int
	height int
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(width, height int) *RenderBuffer {
	size := width * height
	cells := make([]CompositorCell, size)
	for i := range cells {
		cells[i] = emptyCell
	}
	return &RenderBuffer{cells: cells, width: width, height: height}
}

// Resize adjusts buffer dimensions, reallocates only if capacity insufficient
func (b *RenderBuffer) Resize(width, height int) {
	size := width * height
	if cap(b.cells) < size {
		b.cells = make([]CompositorCell, size)
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

// Bounds returns buffer dimensions
func (b *RenderBuffer) Bounds() (width, height int) {
	return b.width, b.height
}

func (b *RenderBuffer) inBounds(x, y int) bool {
	return x >= 0 && x < b.width && y >= 0 && y < b.height
}

// =============================================================================
// COMPOSITOR API (New)
// =============================================================================

// SetPixel composites a pixel with specified blend mode
// mainRune: character to draw (0 preserves existing rune)
// fg, bg: foreground and background RGB colors
// mode: blending algorithm
// alpha: blend factor for BlendAlpha (0.0 = keep dst, 1.0 = use src)
// attrs: text attributes (preserved exactly)
func (b *RenderBuffer) SetPixel(x, y int, mainRune rune, fg, bg RGB, mode BlendMode, alpha float64, attrs tcell.AttrMask) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	// Background blending
	switch mode {
	case BlendReplace:
		dst.Bg = bg
	case BlendAlpha:
		dst.Bg = dst.Bg.Blend(bg, alpha)
	case BlendAdd:
		dst.Bg = dst.Bg.Add(bg)
	case BlendMax:
		dst.Bg = dst.Bg.Max(bg)
	}

	// Foreground handling
	if mainRune != 0 {
		// New rune provided: set rune, fg, and attrs
		dst.Rune = mainRune
		dst.Fg = fg
		dst.Attrs = attrs
	} else {
		// No rune: blend effects only
		switch mode {
		case BlendAdd:
			dst.Fg = dst.Fg.Add(fg)
		case BlendMax:
			dst.Fg = dst.Fg.Max(fg)
		}
		// BlendReplace/BlendAlpha with rune=0: preserve existing fg/attrs
	}
}

// SetSolid is convenience for opaque BlendReplace
func (b *RenderBuffer) SetSolid(x, y int, mainRune rune, fg, bg RGB, attrs tcell.AttrMask) {
	b.SetPixel(x, y, mainRune, fg, bg, BlendReplace, 1.0, attrs)
}

// Get returns cell at (x,y), returns emptyCell on OOB
func (b *RenderBuffer) Get(x, y int) CompositorCell {
	if !b.inBounds(x, y) {
		return emptyCell
	}
	return b.cells[y*b.width+x]
}

// GetRGB returns fg and bg as RGB at (x,y)
func (b *RenderBuffer) GetRGB(x, y int) (fg, bg RGB, attrs tcell.AttrMask) {
	cell := b.Get(x, y)
	return cell.Fg, cell.Bg, cell.Attrs
}

// =============================================================================
// LEGACY BRIDGE API (Temporary - calls compositor internally)
// =============================================================================

// Set writes a rune and style (legacy API, converts tcell.Style to RGB)
func (b *RenderBuffer) Set(x, y int, r rune, style tcell.Style) {
	fgC, bgC, attrs := style.Decompose()
	b.SetPixel(x, y, r, TcellToRGB(fgC), TcellToRGB(bgC), BlendReplace, 1.0, attrs)
}

// SetRune writes a rune preserving existing colors and attrs
func (b *RenderBuffer) SetRune(x, y int, r rune) {
	if !b.inBounds(x, y) {
		return
	}
	b.cells[y*b.width+x].Rune = r
}

// SetString writes a string starting at (x, y), returns runes written
func (b *RenderBuffer) SetString(x, y int, s string, style tcell.Style) int {
	if y < 0 || y >= b.height {
		return 0
	}
	fgC, bgC, attrs := style.Decompose()
	fg, bg := TcellToRGB(fgC), TcellToRGB(bgC)
	written := 0
	for _, r := range s {
		if x >= b.width {
			break
		}
		if x >= 0 {
			b.SetPixel(x, y, r, fg, bg, BlendReplace, 1.0, attrs)
			written++
		}
		x++
	}
	return written
}

// SetMax writes with max-blend (legacy API for materializers/flashes)
func (b *RenderBuffer) SetMax(x, y int, r rune, style tcell.Style) {
	fgC, bgC, attrs := style.Decompose()
	b.SetPixel(x, y, r, TcellToRGB(fgC), TcellToRGB(bgC), BlendMax, 1.0, attrs)
}

// DecomposeAt returns fg, bg, attrs as tcell types (legacy bridge for reading)
func (b *RenderBuffer) DecomposeAt(x, y int) (fg, bg tcell.Color, attrs tcell.AttrMask) {
	cell := b.Get(x, y)
	return RGBToTcell(cell.Fg), RGBToTcell(cell.Bg), cell.Attrs
}

// =============================================================================
// OUTPUT
// =============================================================================

// Flush writes buffer contents to tcell.Screen
func (b *RenderBuffer) Flush(screen tcell.Screen) {
	for y := 0; y < b.height; y++ {
		row := y * b.width
		for x := 0; x < b.width; x++ {
			c := b.cells[row+x]
			style := tcell.StyleDefault.
				Foreground(RGBToTcell(c.Fg)).
				Background(RGBToTcell(c.Bg)).
				Attributes(c.Attrs)
			screen.SetContent(x, y, c.Rune, nil, style)
		}
	}
}