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

	op := uint8(mode) & 0x0F
	flags := uint8(mode) & 0xF0

	// 1. Update Rune/Attrs if provided
	if mainRune != 0 {
		dst.Rune = mainRune
		dst.Attrs = attrs
	}

	// 2. Background Processing
	if flags&flagBg != 0 {
		switch op {
		case opReplace:
			dst.Bg = bg
		case opAlpha:
			dst.Bg = Blend(dst.Bg, bg, alpha)
		case opAdd:
			dst.Bg = Add(dst.Bg, bg)
		case opMax:
			dst.Bg = Max(dst.Bg, bg)
		case opSoftLight:
			dst.Bg = SoftLight(dst.Bg, bg, alpha)
		case opScreen:
			dst.Bg = Screen(dst.Bg, bg)
		case opOverlay:
			dst.Bg = Overlay(dst.Bg, bg)
		}
		// Always mark touched if we touched background
		b.touched[idx] = true
	}

	// 3. Foreground Processing
	if flags&flagFg != 0 {
		switch op {
		case opReplace:
			dst.Fg = fg
		case opAlpha:
			dst.Fg = Blend(dst.Fg, fg, alpha)
		case opAdd:
			dst.Fg = Add(dst.Fg, fg)
		case opMax:
			dst.Fg = Max(dst.Fg, fg)
		case opSoftLight:
			dst.Fg = SoftLight(dst.Fg, fg, alpha)
		case opScreen:
			dst.Fg = Screen(dst.Fg, fg)
		case opOverlay:
			dst.Fg = Overlay(dst.Fg, fg)
		}
	}
}

// SetFgOnly writes rune, foreground, and attrs while preserving existing background
// Unwrapped for performance: Bypass BlendMode decoding and branching for high-frequency text rendering
// Does NOT mark cell as touched, allowing underlying background to persist or default in finalize()
func (b *RenderBuffer) SetFgOnly(x, y int, r rune, fg RGB, attrs terminal.Attr) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	dst.Rune = r
	dst.Fg = fg
	dst.Attrs = attrs
}

// SetBgOnly updates the background color while preserving existing rune/foreground.
// Unwrapped for performance: Optimized for area effects (explosions, UI fills) avoiding full cell writes
// Marks cell as touched to prevent default background override
func (b *RenderBuffer) SetBgOnly(x, y int, bg RGB) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x

	b.cells[idx].Bg = bg
	b.touched[idx] = true
}

// SetWithBg writes a cell with explicit fg and bg colors (opaque replace)
// Unwrapped for performance: This is the "Hot Path" for most rendering (grids, UI, blocks)
// Direct struct assignment avoids overhead of generic Set() function calls
func (b *RenderBuffer) SetWithBg(x, y int, r rune, fg, bg RGB) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	dst.Rune = r
	dst.Fg = fg
	dst.Bg = bg
	dst.Attrs = terminal.AttrNone
	b.touched[idx] = true
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