package render

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// RenderBuffer is a compositor backed by terminal.Cell array with dirty tracking
// Uses []terminal.Cell directly to allow zero-copy export, worth the coupling
type RenderBuffer struct {
	cells   []terminal.Cell // Optimization: Persistent buffer for output reuse
	touched []bool
	width   int
	height  int
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(width, height int) *RenderBuffer {
	size := width * height
	cells := make([]terminal.Cell, size)
	touched := make([]bool, size)
	for i := range cells {
		cells[i] = terminal.Cell{
			Rune:  0,
			Fg:    DefaultBgRGB,
			Bg:    RGBBlack,
			Attrs: terminal.AttrNone,
		}
		touched[i] = false
	}
	return &RenderBuffer{
		cells:   cells,
		touched: touched,
		width:   width,
		height:  height,
	}
}

// Resize adjusts buffer dimensions, reallocates only if capacity insufficient
func (b *RenderBuffer) Resize(width, height int) {
	size := width * height
	if cap(b.cells) < size {
		b.cells = make([]terminal.Cell, size)
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
	b.cells[0] = terminal.Cell{
		Rune:  0,
		Fg:    DefaultBgRGB,
		Bg:    RGBBlack,
		Attrs: terminal.AttrNone,
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
func (b *RenderBuffer) Set(x, y int, mainRune rune, fg, bg RGB, mode BlendMode, alpha float64, attrs terminal.Attr) {
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
		dst.Bg = Blend(dst.Bg, bg, alpha)
		b.touched[idx] = true
	case BlendAdd:
		dst.Bg = Add(dst.Bg, bg)
		b.touched[idx] = true
	case BlendMax:
		dst.Bg = Max(dst.Bg, bg)
		b.touched[idx] = true
	case BlendSoftLight:
		dst.Bg = SoftLight(dst.Bg, bg, alpha)
		b.touched[idx] = true
	case BlendScreen:
		dst.Bg = Screen(dst.Bg, bg)
		b.touched[idx] = true
	case BlendOverlay:
		dst.Bg = Overlay(dst.Bg, bg)
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
			dst.Fg = Add(dst.Bg, fg)
		case BlendMax:
			dst.Fg = Max(dst.Bg, fg)
		}
	}
}

// SetFgOnly writes rune, foreground, and attrs while preserving existing background
// Use for text that floats over backgrounds (grid, shields)
func (b *RenderBuffer) SetFgOnly(x, y int, r rune, fg RGB, attrs terminal.Attr) {
	b.Set(x, y, r, fg, RGBBlack, BlendFgOnly, 0, attrs)
}

// SetWithBg writes a rune with explicit fg and bg colors (opaque replace)
func (b *RenderBuffer) SetWithBg(x, y int, r rune, fg, bg RGB) {
	b.Set(x, y, r, fg, bg, BlendReplace, 1.0, terminal.AttrNone)
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

// FlushToTerminal writes render buffer to terminal
func (b *RenderBuffer) FlushToTerminal(term terminal.Terminal) {
	b.finalize()
	term.Flush(b.cells, b.width, b.height)
}