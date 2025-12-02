package render

import (
	"github.com/gdamore/tcell/v2"
)

// RenderBuffer is a compositor backed by RGB cells with dirty tracking
// Single source of truth - all methods write to the same backing store
type RenderBuffer struct {
	cells   []CompositorCell
	touched []bool
	width   int
	height  int
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(width, height int) *RenderBuffer {
	size := width * height
	cells := make([]CompositorCell, size)
	touched := make([]bool, size)
	for i := range cells {
		cells[i] = CompositorCell{
			Rune:  ' ',
			Fg:    DefaultBgRGB,
			Bg:    RGBBlack,
			Attrs: tcell.AttrNone,
		}
		touched[i] = false
	}
	return &RenderBuffer{cells: cells, touched: touched, width: width, height: height}
}

// Resize adjusts buffer dimensions, reallocates only if capacity insufficient
func (b *RenderBuffer) Resize(width, height int) {
	size := width * height
	if cap(b.cells) < size {
		b.cells = make([]CompositorCell, size)
		b.touched = make([]bool, size)
	} else {
		b.cells = b.cells[:size]
		b.touched = b.touched[:size]
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
	// Initialize first cell
	b.cells[0] = CompositorCell{
		Rune:  ' ',
		Fg:    DefaultBgRGB,
		Bg:    RGBBlack,
		Attrs: tcell.AttrNone,
	}
	b.touched[0] = false
	// Exponential copy for cells
	for filled := 1; filled < len(b.cells); filled *= 2 {
		copy(b.cells[filled:], b.cells[:filled])
	}
	// Exponential copy for touched
	for filled := 1; filled < len(b.touched); filled *= 2 {
		copy(b.touched[filled:], b.touched[:filled])
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

	// Background blending - mark touched for all modes except FgOnly
	switch mode {
	case BlendReplace:
		dst.Bg = bg
		b.touched[idx] = true
	case BlendAlpha:
		dst.Bg = dst.Bg.Blend(bg, alpha)
		b.touched[idx] = true
	case BlendAdd:
		dst.Bg = dst.Bg.Add(bg)
		b.touched[idx] = true
	case BlendMax:
		dst.Bg = dst.Bg.Max(bg)
		b.touched[idx] = true
	case BlendFgOnly:
		// Explicitly preserve destination background, do not mark touched
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

// finalize sets default background to untouched cells before Flush
func (b *RenderBuffer) finalize() {
	for i := range b.cells {
		if !b.touched[i] {
			b.cells[i].Bg = RgbBackground
		}
	}
}

// Flush writes render buffer (full screen frame) to tcell.Screen
func (b *RenderBuffer) Flush(screen tcell.Screen) {
	b.finalize()
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