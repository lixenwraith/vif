package render

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// RenderBuffer is a compositor backed by terminal.Cell array with dirty tracking
type RenderBuffer struct {
	cells       []terminal.Cell
	touched     []bool
	masks       []uint8
	currentMask uint8
	width       int
	height      int
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(width, height int) *RenderBuffer {
	size := width * height
	cells := make([]terminal.Cell, size)
	touched := make([]bool, size)
	masks := make([]uint8, size)
	for i := range cells {
		cells[i] = terminal.Cell{
			Rune:  0,
			Fg:    DefaultBgRGB,
			Bg:    RGBBlack,
			Attrs: terminal.AttrNone,
		}
	}
	return &RenderBuffer{
		cells:       cells,
		touched:     touched,
		masks:       masks,
		currentMask: constant.MaskNone,
		width:       width,
		height:      height,
	}
}

// Resize adjusts buffer dimensions, reallocates only if capacity insufficient
func (b *RenderBuffer) Resize(width, height int) {
	size := width * height
	if cap(b.cells) < size {
		b.cells = make([]terminal.Cell, size)
		b.touched = make([]bool, size)
		b.masks = make([]uint8, size)
	} else {
		b.cells = b.cells[:size]
		b.touched = b.touched[:size]
		b.masks = b.masks[:size]
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
	b.cells[0] = terminal.Cell{
		Rune:  0,
		Fg:    DefaultBgRGB,
		Bg:    RGBBlack,
		Attrs: terminal.AttrNone,
	}
	b.touched[0] = false
	b.masks[0] = constant.MaskNone

	for filled := 1; filled < len(b.cells); filled *= 2 {
		copy(b.cells[filled:], b.cells[:filled])
	}
	for filled := 1; filled < len(b.touched); filled *= 2 {
		copy(b.touched[filled:], b.touched[:filled])
	}
	for filled := 1; filled < len(b.masks); filled *= 2 {
		copy(b.masks[filled:], b.masks[:filled])
	}

	b.currentMask = constant.MaskNone
}

// SetWriteMask sets the mask for subsequent draw operations
func (b *RenderBuffer) SetWriteMask(mask uint8) {
	b.currentMask = mask
}

// inBounds returns true if coordinates are within buffer
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

	b.masks[idx] = b.currentMask

	if mainRune != 0 {
		dst.Rune = mainRune
		dst.Attrs = attrs
	}

	if flags&flagBg != 0 {
		switch op {
		case opReplace:
			dst.Bg = bg
		case opAlpha:
			dst.Bg = Blend(dst.Bg, bg, alpha)
		case opAdd:
			dst.Bg = Add(dst.Bg, bg, alpha)
		case opMax:
			dst.Bg = Max(dst.Bg, bg, alpha)
		case opSoftLight:
			dst.Bg = SoftLight(dst.Bg, bg, alpha)
		case opScreen:
			dst.Bg = Screen(dst.Bg, bg, alpha)
		case opOverlay:
			dst.Bg = Overlay(dst.Bg, bg, alpha)
		}
		b.touched[idx] = true
	}

	if flags&flagFg != 0 {
		switch op {
		case opReplace:
			dst.Fg = fg
		case opAlpha:
			dst.Fg = Blend(dst.Fg, fg, alpha)
		case opAdd:
			dst.Fg = Add(dst.Fg, fg, alpha)
		case opMax:
			dst.Fg = Max(dst.Fg, fg, alpha)
		case opSoftLight:
			dst.Fg = SoftLight(dst.Fg, fg, alpha)
		case opScreen:
			dst.Fg = Screen(dst.Fg, fg, alpha)
		case opOverlay:
			dst.Fg = Overlay(dst.Fg, fg, alpha)
		}
	}
}

// SetFgOnly writes rune, foreground, and attrs while preserving existing background
func (b *RenderBuffer) SetFgOnly(x, y int, r rune, fg RGB, attrs terminal.Attr) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	dst.Rune = r
	dst.Fg = fg
	dst.Attrs = attrs
	b.masks[idx] |= b.currentMask
}

// SetBgOnly updates background color while preserving existing rune/foreground
func (b *RenderBuffer) SetBgOnly(x, y int, bg RGB) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x

	b.cells[idx].Bg = bg
	b.touched[idx] = true
	b.masks[idx] = b.currentMask
}

// SetWithBg writes a cell with explicit fg and bg colors (opaque replace)
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
	b.masks[idx] = b.currentMask
}

// SetBg256 sets background using 256-color palette index directly
// Bypasses RGB conversion for consistent 256-color rendering
func (b *RenderBuffer) SetBg256(x, y int, paletteIdx uint8) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	b.cells[idx].Bg = RGB{R: paletteIdx, G: 0, B: 0}
	b.cells[idx].Attrs = terminal.AttrBg256
	b.touched[idx] = true
	b.masks[idx] = b.currentMask
}

// ===== POST-PROCESSING =====

// MutateDim multiplies colors by factor for cells matching targetMask
// Respects Fg/Bg granularity: touched cells get both mutated, untouched get Fg only
func (b *RenderBuffer) MutateDim(factor float64, targetMask uint8) {
	if factor >= 1.0 {
		return
	}
	for i := range b.cells {
		if b.masks[i]&targetMask == 0 {
			continue
		}
		cell := &b.cells[i]
		cell.Fg = Scale(cell.Fg, factor)
		if b.touched[i] {
			cell.Bg = Scale(cell.Bg, factor)
		}
	}
}

// MutateGrayscale desaturates cells matching targetMask
// intensity: 0.0 = no change, 1.0 = full grayscale
// Respects Fg/Bg granularity: touched cells get both mutated, untouched get Fg only
func (b *RenderBuffer) MutateGrayscale(intensity float64, targetMask uint8) {
	if intensity <= 0.0 {
		return
	}
	fullGray := intensity >= 1.0

	for i := range b.cells {
		if b.masks[i]&targetMask == 0 {
			continue
		}
		cell := &b.cells[i]

		fgGray := Grayscale(cell.Fg)
		if fullGray {
			cell.Fg = fgGray
		} else {
			cell.Fg = Lerp(cell.Fg, fgGray, intensity)
		}

		if b.touched[i] {
			bgGray := Grayscale(cell.Bg)
			if fullGray {
				cell.Bg = bgGray
			} else {
				cell.Bg = Lerp(cell.Bg, bgGray, intensity)
			}
		}
	}
}

// ===== OUTPUT =====

// finalize sets default background to untouched cells and applies occlusion dimming
func (b *RenderBuffer) finalize() {
	for i := range b.cells {
		if !b.touched[i] {
			b.cells[i].Bg = RgbBackground
		} else if constant.OcclusionDimEnabled && b.cells[i].Rune != 0 {
			// Dim background when foreground character present
			if b.masks[i]&(constant.OcclusionDimMask) != 0 {
				b.cells[i].Bg = Scale(b.cells[i].Bg, constant.OcclusionDimFactor)
			}
		}
	}
}

// FlushToTerminal writes render buffer to terminal
func (b *RenderBuffer) FlushToTerminal(term terminal.Terminal) {
	b.finalize()
	term.Flush(b.cells, b.width, b.height)
}