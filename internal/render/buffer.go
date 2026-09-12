package render

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
)

// backgroundOverlay holds deferred background effect state
type backgroundOverlay struct {
	active    bool
	bgColor   color.RGB
	intensity float64
}

// voidRegion is the part of the game area that no simulation cell covers: the
// margin a map smaller than the viewport is centred in. Left at the theme
// background it reads as playable space, so finalize paints it separately.
type voidRegion struct {
	active    bool
	area      Rect
	playfield Rect
	bgColor   color.RGB
}

// RenderBuffer is a compositor backed by terminal.Cell array with dirty tracking
type RenderBuffer struct {
	colorMode    terminal.ColorMode
	cells        []terminal.Cell
	touched      []bool
	masks        []uint8
	currentMask  uint8
	width        int
	height       int
	clip         Rect
	bgOverlay    backgroundOverlay
	void         voidRegion
	finalizeFunc func(*RenderBuffer)
}

// NewRenderBuffer creates a buffer with the specified dimensions
func NewRenderBuffer(colorMode terminal.ColorMode, width, height int) *RenderBuffer {
	size := width * height
	b := &RenderBuffer{
		colorMode:   colorMode,
		cells:       make([]terminal.Cell, size),
		touched:     make([]bool, size),
		masks:       make([]uint8, size),
		currentMask: visual.MaskNone,
		width:       width,
		height:      height,
		clip:        RectWH(0, 0, width, height),
	}
	if colorMode == terminal.ColorModeTrueColor && visual.OcclusionDimEnabled {
		b.finalizeFunc = finalizeTrueColorOcclusion
	} else {
		b.finalizeFunc = finalizeSimple
	}
	return b
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

// Clear resets all cells to empty and zero-initializes metadata.
// The write clip is reopened to the whole buffer: a frame starts with no layer
// selected, so no layer's restriction may outlive the frame that set it.
func (b *RenderBuffer) Clear() {
	b.clip = b.Bounds()
	b.void = voidRegion{}

	if len(b.cells) == 0 {
		return
	}

	clear(b.cells)
	clear(b.touched)
	clear(b.masks)
	b.currentMask = visual.MaskNone
	b.bgOverlay = backgroundOverlay{}
}

// Bounds returns the whole buffer as a rectangle in buffer coordinates
func (b *RenderBuffer) Bounds() Rect { return RectWH(0, 0, b.width, b.height) }

// SetClip restricts every subsequent write to the intersection of r with the
// buffer. It is how the orchestrator confines simulation layers to the cells the
// map covers, so a renderer drawing a shape around an entity near the map edge
// cannot spill into the non-playable margin without knowing the margin exists.
func (b *RenderBuffer) SetClip(r Rect) {
	b.clip = r.Intersect(b.Bounds())
}

// ClearClip reopens the whole buffer for writing, for layers that address the
// screen rather than the map: post-processing, UI, and debug projections
func (b *RenderBuffer) ClearClip() { b.clip = b.Bounds() }

// Clip returns the currently writable rectangle in buffer coordinates
func (b *RenderBuffer) Clip() Rect { return b.clip }

// SetVoidRegion declares the part of area outside playfield as non-playable, so
// finalize fills the cells nothing drew with c rather than the theme background.
// An area the playfield already covers clears the declaration.
func (b *RenderBuffer) SetVoidRegion(area, playfield Rect, c color.RGB) {
	area = area.Intersect(b.Bounds())
	if area.Empty() || playfield.Intersect(area) == area {
		b.void = voidRegion{}
		return
	}
	b.void = voidRegion{active: true, area: area, playfield: playfield, bgColor: c}
}

// CellAt reads one composited cell, or the zero cell when the coordinate is
// outside the buffer. It is the read half of the write API: a renderer composes
// through Set, and a test or a diagnostic reads back what it composed without
// needing a terminal to flush to.
func (b *RenderBuffer) CellAt(x, y int) terminal.Cell {
	// Buffer bounds rather than the write clip: reading back is not drawing, and
	// a caller checking that a layer stayed inside its clip must be able to read
	// the cells outside it.
	if !b.Bounds().Contains(x, y) {
		return terminal.Cell{}
	}
	return b.cells[y*b.width+x]
}

// BackgroundAt returns the composed background, substituting fallback for an
// untouched cell whose staging value remains zero until finalization.
func (b *RenderBuffer) BackgroundAt(x, y int, fallback color.RGB) color.RGB {
	if !b.Bounds().Contains(x, y) {
		return fallback
	}
	idx := y*b.width + x
	if !b.touched[idx] {
		return fallback
	}
	return b.cells[idx].Bg
}

// SetWriteMask sets the mask for subsequent draw operations
func (b *RenderBuffer) SetWriteMask(mask uint8) {
	b.currentMask = mask
}

// ResetMask replaces a cell's mask with the current write mask, discarding
// tags left by lower layers so post-processing treats the cell as this layer only
func (b *RenderBuffer) ResetMask(x, y int) {
	if !b.inBounds(x, y) {
		return
	}
	b.masks[y*b.width+x] = b.currentMask
}

// SetBackgroundOverlay configures overlay for untouched cells applied in finalize()
// Intensity is pre-computed by caller (envelope already applied)
func (b *RenderBuffer) SetBackgroundOverlay(c color.RGB, intensity float64) {
	b.bgOverlay = backgroundOverlay{
		active:    true,
		bgColor:   c,
		intensity: intensity,
	}
}

// inBounds returns true if coordinates are writable: inside the buffer and
// inside the active clip, which SetClip keeps intersected with the buffer so
// this stays one range test rather than two
func (b *RenderBuffer) inBounds(x, y int) bool {
	return b.clip.Contains(x, y)
}

// === COMPOSITOR API ===

// Set composites a cell with specified blend mode
func (b *RenderBuffer) Set(x, y int, mainRune rune, fg, bg color.RGB, mode BlendMode, alpha float64, attrs terminal.Attr) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	op := uint8(mode) & 0x0F
	flags := uint8(mode) & 0xF0

	b.masks[idx] |= b.currentMask

	if mainRune != 0 {
		dst.Rune = mainRune
		// Preserve 256-color attr for unaffected channel
		switch flags {
		case flagFg:
			// Fg-only blend: preserve bg attrs
			dst.Attrs = (dst.Attrs & terminal.AttrBg256) | attrs
		case flagBg:
			// Bg-only blend: preserve fg attrs
			dst.Attrs = (dst.Attrs & terminal.AttrFg256) | attrs
		default:
			// Both channels: use provided attrs
			dst.Attrs = attrs
		}
	}

	if flags&flagBg != 0 {
		switch op {
		case opReplace:
			dst.Bg = bg
		case opAlpha:
			dst.Bg = color.Blend(dst.Bg, bg, alpha)
		case opAdd:
			dst.Bg = color.Add(dst.Bg, bg, alpha)
		case opMax:
			dst.Bg = color.Max(dst.Bg, bg, alpha)
		case opSoftLight:
			dst.Bg = color.SoftLight(dst.Bg, bg, alpha)
		case opScreen:
			dst.Bg = color.Screen(dst.Bg, bg, alpha)
		case opOverlay:
			dst.Bg = color.Overlay(dst.Bg, bg, alpha)
		}
		b.touched[idx] = true
	}

	if flags&flagFg != 0 {
		switch op {
		case opReplace:
			dst.Fg = fg
		case opAlpha:
			dst.Fg = color.Blend(dst.Fg, fg, alpha)
		case opAdd:
			dst.Fg = color.Add(dst.Fg, fg, alpha)
		case opMax:
			dst.Fg = color.Max(dst.Fg, fg, alpha)
		case opSoftLight:
			dst.Fg = color.SoftLight(dst.Fg, fg, alpha)
		case opScreen:
			dst.Fg = color.Screen(dst.Fg, fg, alpha)
		case opOverlay:
			dst.Fg = color.Overlay(dst.Fg, fg, alpha)
		}
	}
}

// SetFgOnly writes rune, foreground, and attrs while preserving existing background and bg attrs
func (b *RenderBuffer) SetFgOnly(x, y int, r rune, fg color.RGB, attrs terminal.Attr) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]

	dst.Rune = r
	dst.Fg = fg
	// Preserve bg-related attrs (AttrBg256), combine with new fg attrs
	dst.Attrs = (dst.Attrs & terminal.AttrBg256) | attrs
	b.masks[idx] |= b.currentMask
}

// SetBgOnly updates background color while preserving existing rune/foreground
func (b *RenderBuffer) SetBgOnly(x, y int, bg color.RGB) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x

	b.cells[idx].Bg = bg
	b.touched[idx] = true
	b.masks[idx] |= b.currentMask
}

// SetBgScreen screen-blends a background, using base only when no lower
// renderer has supplied the cell's background yet.
func (b *RenderBuffer) SetBgScreen(x, y int, bg, base color.RGB, alpha float64) {
	if alpha <= 0 || !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	dst := &b.cells[idx]
	if !b.touched[idx] {
		dst.Bg = base
	}

	dst.Bg = color.Screen(dst.Bg, bg, alpha)
	b.touched[idx] = true
	b.masks[idx] |= b.currentMask
}

// SetWithBg writes a cell with explicit fg and bg colors (opaque replace)
func (b *RenderBuffer) SetWithBg(x, y int, r rune, fg, bg color.RGB) {
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
	// b.masks[idx] |= b.currentMask // changed due to game leaking to overlay, test if other things break
	b.masks[idx] = b.currentMask
}

// SetBg256 sets background using 256-color palette index directly
// Preserves fg-related attrs (AttrFg256) for layered rendering
func (b *RenderBuffer) SetBg256(x, y int, paletteIdx uint8) {
	if !b.inBounds(x, y) {
		return
	}
	idx := y*b.width + x
	b.cells[idx].Bg = color.RGB{R: paletteIdx, G: 0, B: 0}
	// Preserve fg-related attrs, add bg256
	b.cells[idx].Attrs = (b.cells[idx].Attrs & terminal.AttrFg256) | terminal.AttrBg256
	b.touched[idx] = true
	b.masks[idx] |= b.currentMask
}

// === POST-PROCESSING ===

// MutateDim multiplies colors by factor for cells matching targetMask
// Skips 256-color palette cells to prevent palette index corruption
// Respects Fg/Bg granularity: touched cells get both mutated, untouched get Fg only
func (b *RenderBuffer) MutateDim(factor float64, targetMask uint8) {
	if factor >= 1.0 {
		return
	}
	for y := b.clip.Y0; y < b.clip.Y1; y++ {
		row := y * b.width
		for x := b.clip.X0; x < b.clip.X1; x++ {
			i := row + x
			if b.masks[i]&targetMask == 0 {
				continue
			}
			cell := &b.cells[i]

			// Skip 256-color fg - scaling palette index corrupts color
			if cell.Attrs&terminal.AttrFg256 == 0 {
				cell.Fg = color.Scale(cell.Fg, factor)
			}

			if b.touched[i] && cell.Attrs&terminal.AttrBg256 == 0 {
				cell.Bg = color.Scale(cell.Bg, factor)
			}
		}
	}
}

// MutateGrayscale desaturates cells matching targetMask
// intensity: 0.0 = no change, 1.0 = full grayscale
// Skips 256-color palette cells to prevent palette index corruption
// Respects Fg/Bg granularity: touched cells get both mutated, untouched get Fg only
func (b *RenderBuffer) MutateGrayscale(intensity float64, targetMask, excludeMask uint8) {
	if intensity <= 0.0 {
		return
	}
	fullGray := intensity >= 1.0

	for y := b.clip.Y0; y < b.clip.Y1; y++ {
		row := y * b.width
		for x := b.clip.X0; x < b.clip.X1; x++ {
			i := row + x
			if b.masks[i]&targetMask == 0 {
				continue
			}
			if excludeMask != 0 && b.masks[i]&excludeMask != 0 {
				continue
			}
			cell := &b.cells[i]

			// Skip 256-color fg - grayscale conversion corrupts palette index
			if cell.Attrs&terminal.AttrFg256 == 0 {
				fgGray := color.Grayscale(cell.Fg)
				if fullGray {
					cell.Fg = fgGray
				} else {
					cell.Fg = color.Lerp(cell.Fg, fgGray, intensity)
				}
			}

			if b.touched[i] && cell.Attrs&terminal.AttrBg256 == 0 {
				bgGray := color.Grayscale(cell.Bg)
				if fullGray {
					cell.Bg = bgGray
				} else {
					cell.Bg = color.Lerp(cell.Bg, bgGray, intensity)
				}
			}
		}
	}
}

// === OUTPUT ===

// finalize delegates to the appropriate implementation selected at init, then
// repaints the non-playable margin over the background it just filled
func (b *RenderBuffer) finalize() {
	b.finalizeFunc(b)
	b.fillVoid()
}

// fillVoid paints the declared non-playable margin. Only cells nothing drew are
// repainted, so a UI or debug layer that legitimately reaches into the margin —
// an overlay panel, a pinned readout — keeps the colours it composed. The four
// bands are walked rather than the whole area so the playfield, which is most of
// the game area, is never visited.
func (b *RenderBuffer) fillVoid() {
	if !b.void.active {
		return
	}
	a, p := b.void.area, b.void.playfield

	// Rows entirely above and below the playfield, then the left and right
	// margins of the rows that cross it.
	b.fillVoidBand(a.X0, a.X1, a.Y0, min(p.Y0, a.Y1))
	b.fillVoidBand(a.X0, a.X1, max(p.Y1, a.Y0), a.Y1)
	midY0, midY1 := max(p.Y0, a.Y0), min(p.Y1, a.Y1)
	b.fillVoidBand(a.X0, min(p.X0, a.X1), midY0, midY1)
	b.fillVoidBand(max(p.X1, a.X0), a.X1, midY0, midY1)
}

// fillVoidBand fills one half-open span of untouched cells with the void colour
func (b *RenderBuffer) fillVoidBand(x0, x1, y0, y1 int) {
	for y := y0; y < y1; y++ {
		row := y * b.width
		for x := x0; x < x1; x++ {
			if b.touched[row+x] {
				continue
			}
			b.cells[row+x].Bg = b.void.bgColor
		}
	}
}

// finalizeTrueColorOcclusion handles untouched backgrounds and occlusion dimming
func finalizeTrueColorOcclusion(b *RenderBuffer) {
	// Pre-compute untouched background once
	untouchedBg := visual.RgbBackground
	if b.bgOverlay.active {
		untouchedBg = color.Scale(b.bgOverlay.bgColor, b.bgOverlay.intensity)
	}

	for i := range b.cells {
		if !b.touched[i] {
			b.cells[i].Bg = untouchedBg
			continue
		}
		// Occlusion dimming for touched cells with foreground
		// if b.cells[i].Rune != 0 && b.masks[i]&visual.OcclusionDimMask != 0 && b.masks[i]&visual.MaskUI == 0 {
		if b.cells[i].Rune != 0 && b.masks[i]&visual.OcclusionDimMask != 0 {
			b.cells[i].Bg = color.Scale(b.cells[i].Bg, visual.OcclusionDimFactor)
		}
	}
}

// finalizeSimple handles untouched backgrounds only (256-color or no occlusion)
func finalizeSimple(b *RenderBuffer) {
	untouchedBg := visual.RgbBackground
	if b.bgOverlay.active {
		untouchedBg = color.Scale(b.bgOverlay.bgColor, b.bgOverlay.intensity)
	}

	for i := range b.cells {
		if !b.touched[i] {
			b.cells[i].Bg = untouchedBg
		}
	}
}

// FlushToTerminal writes render buffer to terminal
func (b *RenderBuffer) FlushToTerminal(term terminal.Terminal) {
	b.finalize()
	term.Flush(b.cells, b.width, b.height)
}
