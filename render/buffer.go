package render

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/core"
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

// Bounds returns buffer dimensions
func (b *RenderBuffer) Bounds() (width, height int) {
	return b.width, b.height
}

func (b *RenderBuffer) inBounds(x, y int) bool {
	return x >= 0 && x < b.width && y >= 0 && y < b.height
}

// =============================================================================
// COMPOSITOR API
// =============================================================================

// SetPixel composites a pixel with specified blend mode
// mainRune: character to draw (0 preserves existing rune)
// fg, bg: foreground and background RGB colors
// mode: blending algorithm
// alpha: blend factor for BlendAlpha (0.0 = keep dst, 1.0 = use src)
// attrs: text attributes (preserved exactly)
func (b *RenderBuffer) SetPixel(x, y int, mainRune rune, fg, bg core.RGB, mode BlendMode, alpha float64, attrs tcell.AttrMask) {
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

// Get returns cell at (x,y), returns emptyCell on OOB
func (b *RenderBuffer) Get(x, y int) CompositorCell {
	if !b.inBounds(x, y) {
		return emptyCell
	}
	return b.cells[y*b.width+x]
}

// =============================================================================
// LEGACY BRIDGE API (Temporary - to be removed after full migration)
// =============================================================================

// Set writes a rune with fg/bg colors (legacy bridge)
func (b *RenderBuffer) Set(x, y int, r rune, fg core.RGB) {
	b.SetPixel(x, y, r, fg, DefaultBgRGB, BlendReplace, 1.0, tcell.AttrNone)
}

// SetWithBg writes a rune with explicit fg and bg colors
func (b *RenderBuffer) SetWithBg(x, y int, r rune, fg, bg core.RGB) {
	b.SetPixel(x, y, r, fg, bg, BlendReplace, 1.0, tcell.AttrNone)
}

// SetWithAttrs writes a rune with fg, bg, and attributes
func (b *RenderBuffer) SetWithAttrs(x, y int, r rune, fg, bg core.RGB, attrs tcell.AttrMask) {
	b.SetPixel(x, y, r, fg, bg, BlendReplace, 1.0, attrs)
}

// =============================================================================
// OUTPUT
// =============================================================================

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

// RGBToTcell converts RGB to tcell.Color (used only in Flush)
func RGBToTcell(rgb core.RGB) tcell.Color {
	return tcell.NewRGBColor(int32(rgb.R), int32(rgb.G), int32(rgb.B))
}