package ascimage

import (
	"fmt"
	"image"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// ViewMode determines how the image is displayed
type ViewMode uint8

const (
	ViewFit    ViewMode = iota // Scale to fit terminal
	ViewActual                 // 1:1 pixel mapping (with panning)
	ViewCustom                 // Custom zoom level
)

// Viewer manages image display with viewport and navigation
type Viewer struct {
	img       image.Image
	srcWidth  int
	srcHeight int

	// Converted image cache
	converted *ConvertedImage
	convWidth int // Width used for conversion

	// Display settings
	RenderMode RenderMode
	ColorMode  terminal.ColorMode
	ViewMode   ViewMode
	ZoomLevel  int // Percentage: 100 = 1:1, 50 = half size

	// Viewport for panning (top-left corner of view)
	ViewportX int
	ViewportY int

	// Status line
	ShowStatus bool
}

// NewViewer creates a viewer for the given image
func NewViewer(img image.Image) *Viewer {
	bounds := img.Bounds()
	return &Viewer{
		img:        img,
		srcWidth:   bounds.Dx(),
		srcHeight:  bounds.Dy(),
		RenderMode: ModeQuadrant,
		ColorMode:  terminal.ColorModeTrueColor,
		ViewMode:   ViewFit,
		ZoomLevel:  100,
		ShowStatus: true,
	}
}

// ImageSize returns source image dimensions
func (v *Viewer) ImageSize() (int, int) {
	return v.srcWidth, v.srcHeight
}

// ConvertedSize returns current converted image dimensions
func (v *Viewer) ConvertedSize() (int, int) {
	if v.converted == nil {
		return 0, 0
	}
	return v.converted.Width, v.converted.Height
}

// calculateTargetWidth determines output width based on view mode and terminal size
func (v *Viewer) calculateTargetWidth(termW, termH int) int {
	// Reserve 1 row for status if enabled
	availH := termH
	if v.ShowStatus {
		availH--
	}

	switch v.ViewMode {
	case ViewFit:
		// Find width that makes image fit both dimensions
		// Try terminal width first
		_, h := CalculateOutputSize(v.srcWidth, v.srcHeight, termW)
		if h <= availH {
			return termW
		}
		// Scale down to fit height
		// h = w * (srcH/srcW) * 0.5 => w = h * 2 * srcW / srcH
		w := (availH * 2 * v.srcWidth) / v.srcHeight
		if w < 1 {
			w = 1
		}
		return w

	case ViewActual:
		// 1 terminal column per source pixel (or 2 for quadrant mode's effective resolution)
		if v.RenderMode == ModeQuadrant {
			return (v.srcWidth + 1) / 2 // 2 source pixels per cell horizontally
		}
		return v.srcWidth

	case ViewCustom:
		baseW := v.srcWidth
		if v.RenderMode == ModeQuadrant {
			baseW = (v.srcWidth + 1) / 2
		}
		w := (baseW * v.ZoomLevel) / 100
		if w < 1 {
			w = 1
		}
		return w
	}

	return termW
}

// Update reconverts the image if parameters changed
func (v *Viewer) Update(termW, termH int) {
	targetW := v.calculateTargetWidth(termW, termH)

	// Skip if already converted at this width
	if v.converted != nil && v.convWidth == targetW {
		return
	}

	v.converted = ConvertImage(v.img, targetW, v.RenderMode, v.ColorMode)
	v.convWidth = targetW

	// Clamp viewport after resize
	v.clampViewport(termW, termH)
}

// ForceUpdate forces reconversion (e.g., after mode change)
func (v *Viewer) ForceUpdate(termW, termH int) {
	v.converted = nil
	v.convWidth = 0
	v.Update(termW, termH)
}

// clampViewport ensures viewport stays within bounds
func (v *Viewer) clampViewport(termW, termH int) {
	if v.converted == nil {
		v.ViewportX = 0
		v.ViewportY = 0
		return
	}

	availH := termH
	if v.ShowStatus {
		availH--
	}

	maxX := v.converted.Width - termW
	maxY := v.converted.Height - availH

	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}

	if v.ViewportX < 0 {
		v.ViewportX = 0
	}
	if v.ViewportX > maxX {
		v.ViewportX = maxX
	}
	if v.ViewportY < 0 {
		v.ViewportY = 0
	}
	if v.ViewportY > maxY {
		v.ViewportY = maxY
	}
}

// Pan moves the viewport by delta
func (v *Viewer) Pan(dx, dy int, termW, termH int) {
	v.ViewportX += dx
	v.ViewportY += dy
	v.clampViewport(termW, termH)
}

// PanTo moves viewport to absolute position
func (v *Viewer) PanTo(x, y int, termW, termH int) {
	v.ViewportX = x
	v.ViewportY = y
	v.clampViewport(termW, termH)
}

// CenterViewport centers the image in viewport if smaller than terminal
func (v *Viewer) CenterViewport(termW, termH int) {
	if v.converted == nil {
		return
	}

	availH := termH
	if v.ShowStatus {
		availH--
	}

	// If image is smaller, viewport stays at 0
	// Centering offset is handled in Render
	v.ViewportX = 0
	v.ViewportY = 0
}

// ToggleViewMode cycles through view modes
func (v *Viewer) ToggleViewMode() {
	switch v.ViewMode {
	case ViewFit:
		v.ViewMode = ViewActual
	case ViewActual:
		v.ViewMode = ViewFit
	case ViewCustom:
		v.ViewMode = ViewFit
	}
	v.ViewportX = 0
	v.ViewportY = 0
}

// ToggleRenderMode cycles render modes
func (v *Viewer) ToggleRenderMode() {
	if v.RenderMode == ModeBackgroundOnly {
		v.RenderMode = ModeQuadrant
	} else {
		v.RenderMode = ModeBackgroundOnly
	}
}

// ToggleColorMode cycles color modes
func (v *Viewer) ToggleColorMode() {
	if v.ColorMode == terminal.ColorModeTrueColor {
		v.ColorMode = terminal.ColorMode256
	} else {
		v.ColorMode = terminal.ColorModeTrueColor
	}
}

// AdjustZoom changes zoom level by delta percent
func (v *Viewer) AdjustZoom(delta int) {
	v.ViewMode = ViewCustom
	v.ZoomLevel += delta
	if v.ZoomLevel < 10 {
		v.ZoomLevel = 10
	}
	if v.ZoomLevel > 400 {
		v.ZoomLevel = 400
	}
}

// Render draws the image to the render buffer
func (v *Viewer) Render(buf *render.RenderBuffer, termW, termH int) {
	if v.converted == nil {
		return
	}

	availH := termH
	if v.ShowStatus {
		availH--
	}

	// Calculate centering offset for images smaller than terminal
	offsetX := 0
	offsetY := 0
	if v.converted.Width < termW {
		offsetX = (termW - v.converted.Width) / 2
	}
	if v.converted.Height < availH {
		offsetY = (availH - v.converted.Height) / 2
	}

	// Copy visible portion of converted image
	for y := 0; y < availH; y++ {
		srcY := y + v.ViewportY - offsetY
		if srcY < 0 || srcY >= v.converted.Height {
			continue
		}

		for x := 0; x < termW; x++ {
			srcX := x + v.ViewportX - offsetX
			if srcX < 0 || srcX >= v.converted.Width {
				continue
			}

			srcIdx := srcY*v.converted.Width + srcX
			cell := v.converted.Cells[srcIdx]

			buf.Set(x, y, cell.Rune, cell.Fg, cell.Bg, render.BlendReplace, 1.0, cell.Attrs)
		}
	}

	// Render status line
	if v.ShowStatus {
		v.renderStatus(buf, termW, termH)
	}
}

// renderStatus draws the status line at bottom of screen
func (v *Viewer) renderStatus(buf *render.RenderBuffer, termW, termH int) {
	y := termH - 1

	// Status bar background
	statusBg := terminal.RGB{R: 40, G: 40, B: 50}
	statusFg := terminal.RGB{R: 200, G: 200, B: 200}
	keyFg := terminal.RGB{R: 100, G: 180, B: 255}

	for x := 0; x < termW; x++ {
		buf.Set(x, y, ' ', statusFg, statusBg, render.BlendReplace, 1.0, terminal.AttrNone)
	}

	// Build status text
	viewStr := "Fit"
	if v.ViewMode == ViewActual {
		viewStr = "1:1"
	} else if v.ViewMode == ViewCustom {
		viewStr = fmt.Sprintf("%d%%", v.ZoomLevel)
	}

	colorStr := "24bit"
	if v.ColorMode == terminal.ColorMode256 {
		colorStr = "256"
	}

	var convW, convH int
	if v.converted != nil {
		convW, convH = v.converted.Width, v.converted.Height
	}

	status := fmt.Sprintf(" %dx%d → %dx%d | %s | %s | %s ",
		v.srcWidth, v.srcHeight, convW, convH,
		v.RenderMode.String(), colorStr, viewStr)

	// Position info for panning
	if v.converted != nil && (v.converted.Width > termW || v.converted.Height > termH-1) {
		status += fmt.Sprintf("| [%d,%d] ", v.ViewportX, v.ViewportY)
	}

	// Help keys
	help := " q:quit f:fit m:mode c:color ±:zoom arrows:pan"

	// Write status (left-aligned)
	x := 0
	for _, r := range status {
		if x >= termW {
			break
		}
		buf.SetFgOnly(x, y, r, statusFg, terminal.AttrNone)
		x++
	}

	// Write help (right-aligned)
	helpStart := termW - len(help)
	if helpStart > x {
		x = helpStart
		for _, r := range help {
			if x >= termW {
				break
			}
			fg := statusFg
			if r == ':' || (r >= 'a' && r <= 'z') {
				fg = keyFg
			}
			buf.SetFgOnly(x, y, r, fg, terminal.AttrNone)
			x++
		}
	}
}

// NeedsPanning returns true if image exceeds viewport
func (v *Viewer) NeedsPanning(termW, termH int) bool {
	if v.converted == nil {
		return false
	}
	availH := termH
	if v.ShowStatus {
		availH--
	}
	return v.converted.Width > termW || v.converted.Height > availH
}