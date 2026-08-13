package engine

import "github.com/lixenwraith/vi-fighter/internal/parameter"

// OverlayGeometry is the resolved overlay window placement for one terminal size.
// X and Y are screen coordinates; every other coordinate is relative to the window.
type OverlayGeometry struct {
	X, Y, W, H         int  // Window rect
	ContentX, ContentY int  // Scrollable body origin
	ContentW, ContentH int  // Scrollable body size
	ScrollX, ScrollW   int  // Scrollbar column; ScrollW is 0 when dropped
	HintY              int  // Hint row, -1 when it does not fit
	Valid              bool // False when the terminal is too small to draw
}

// ComputeOverlayGeometry resolves the overlay window for a terminal size.
// Pure over parameter constants so renderer and router agree without sharing state.
func ComputeOverlayGeometry(screenW, screenH int) OverlayGeometry {
	w := overlayAxis(screenW, parameter.OverlayWidthPercent,
		parameter.OverlayMinWidth, parameter.OverlayMaxWidth, parameter.OverlayScreenMarginX)
	h := overlayAxis(screenH, parameter.OverlayHeightPercent,
		parameter.OverlayMinHeight, parameter.OverlayMaxHeight, parameter.OverlayScreenMarginY)

	if w < parameter.OverlayUsableWidth || h < parameter.OverlayUsableHeight {
		return OverlayGeometry{}
	}

	g := OverlayGeometry{
		X: (screenW - w) / 2, Y: (screenH - h) / 2,
		W: w, H: h,
		HintY: -1,
		Valid: true,
	}

	// Interior rows run between the borders; the hint owns the last one, so no
	// dead row sits under it
	top := 1 + parameter.OverlayPaddingY
	last := h - 2
	if last < top {
		top = 1
	}
	g.ContentY = top
	g.ContentH = last - top + 1
	if g.ContentH > parameter.OverlayHintRows {
		g.HintY = last
		g.ContentH -= parameter.OverlayHintRows
	}

	// Content keeps its full padded width; the scrollbar sits in the right gutter
	g.ContentX = 1 + parameter.OverlayPaddingX
	usableW := w - 2 - 2*parameter.OverlayPaddingX
	if usableW < 1 {
		g.ContentX, usableW = 1, w-2
	}
	scrollX := w - 1 - parameter.OverlayScrollbarMargin
	if usableW >= parameter.OverlayScrollbarMinWidth && g.ContentX+usableW <= scrollX {
		g.ScrollW, g.ScrollX = 1, scrollX
	}
	g.ContentW = usableW

	return g
}

// overlayAxis scales one axis by percentage, applies the floor and cap,
// then bounds it by the screen minus its edge margin
func overlayAxis(screen int, pct float64, minV, maxV, margin int) int {
	avail := screen - 2*margin
	if avail < 1 {
		avail = screen
	}
	v := max(int(float64(screen)*pct), minV)
	if maxV > 0 {
		v = min(v, maxV)
	}
	return min(v, avail)
}
