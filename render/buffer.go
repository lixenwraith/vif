package render

import (
	"github.com/gdamore/tcell/v2"
)

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

// inBounds returns true if in screen bounds
func (b *RenderBuffer) inBounds(x, y int) bool {
	return x >= 0 && x < b.width && y >= 0 && y < b.height
}

// ===== COMPOSITOR API =====

// Set composites a cell with specified blend mode
func (b *RenderBuffer) Set(x, y int, mainRune rune, fg, bg RGB, mode BlendMode, alpha float64, attrs tcell.AttrMask) {
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
	case BlendFgOnly:
		// Explicitly preserve destination background
	}

	// Foreground handling
	if mainRune != 0 {
		dst.Rune = mainRune
		dst.Fg = fg
		dst.Attrs = attrs
	} else if mode != BlendFgOnly {
		switch mode {
		case BlendAdd:
			dst.Fg = dst.Fg.Add(fg)
		case BlendMax:
			dst.Fg = dst.Fg.Max(fg)
		}
	}
}

// SetFgOnly writes rune, foreground, and attrs while preserving existing background
// Use for text that floats over backgrounds (grid, shields)
func (b *RenderBuffer) SetFgOnly(x, y int, r rune, fg RGB, attrs tcell.AttrMask) {
	b.Set(x, y, r, fg, RGBBlack, BlendFgOnly, 0, attrs)
}

// SetWithBg writes a rune with explicit fg and bg colors (opaque replace)
func (b *RenderBuffer) SetWithBg(x, y int, r rune, fg, bg RGB) {
	b.Set(x, y, r, fg, bg, BlendReplace, 1.0, tcell.AttrNone)
}

// ===== OUTPUT =====

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

// RGBToTcell converts RGB to tcell.Color
func RGBToTcell(rgb RGB) tcell.Color {
	return tcell.NewRGBColor(int32(rgb.R), int32(rgb.G), int32(rgb.B))
}