package main

import (
	"fmt"
	"image"

	"github.com/lixenwraith/ascimage"
	lcolor "github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
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
	img     image.Image
	dualImg *ascimage.DualModeImage

	srcWidth  int
	srcHeight int

	converted *ascimage.ConvertedImage
	convWidth int
	aspect    float64

	RenderMode ascimage.RenderMode
	ColorMode  terminal.ColorMode
	ViewMode   ViewMode
	ZoomLevel  int

	ViewportX  int
	ViewportY  int
	ShowStatus bool
}

// NewViewer creates a viewer for the given image, converting at a cell aspect
func NewViewer(img image.Image, aspect float64) *Viewer {
	bounds := img.Bounds()
	return &Viewer{
		img:        img,
		aspect:     aspect,
		srcWidth:   bounds.Dx(),
		srcHeight:  bounds.Dy(),
		RenderMode: ascimage.ModeQuadrant,
		ColorMode:  terminal.ColorModeTrueColor,
		ViewMode:   ViewFit,
		ZoomLevel:  100,
		ShowStatus: true,
	}
}

// NewViewerFromDual creates a viewer for a pre-converted .aimg file
func NewViewerFromDual(dual *ascimage.DualModeImage) *Viewer {
	return &Viewer{
		dualImg:    dual,
		srcWidth:   dual.Width,
		srcHeight:  dual.Height,
		RenderMode: dual.RenderMode,
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

func (v *Viewer) calculateTargetWidth(termW, termH int) int {
	availH := termH
	if v.ShowStatus {
		availH--
	}

	switch v.ViewMode {
	case ViewFit:
		_, h := ascimage.CalculateOutputSize(v.srcWidth, v.srcHeight, termW, v.aspect)
		if h <= availH {
			return termW
		}
		w := int(float64(availH*v.srcWidth)/v.aspect) / v.srcHeight
		if w < 1 {
			w = 1
		}
		return w

	case ViewActual:
		if v.RenderMode == ascimage.ModeQuadrant {
			return (v.srcWidth + 1) / 2
		}
		return v.srcWidth

	case ViewCustom:
		baseW := v.srcWidth
		if v.RenderMode == ascimage.ModeQuadrant {
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
	if v.dualImg != nil {
		if v.converted == nil {
			v.converted = v.dualImg.ToConvertedImage(v.ColorMode)
			v.convWidth = v.converted.Width
		}
		v.clampViewport(termW, termH)
		return
	}

	targetW := v.calculateTargetWidth(termW, termH)
	if v.converted != nil && v.convWidth == targetW {
		return
	}

	v.converted = ascimage.ConvertImage(v.img, targetW, v.RenderMode, v.ColorMode, v.aspect)
	v.convWidth = targetW
	v.clampViewport(termW, termH)
}

// ForceUpdate forces reconversion
func (v *Viewer) ForceUpdate(termW, termH int) {
	if v.dualImg != nil {
		v.converted = v.dualImg.ToConvertedImage(v.ColorMode)
		v.convWidth = v.converted.Width
		v.clampViewport(termW, termH)
		return
	}
	v.converted = nil
	v.convWidth = 0
	v.Update(termW, termH)
}

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

// ToggleViewMode cycles through view modes
func (v *Viewer) ToggleViewMode() {
	if v.dualImg != nil {
		return
	}
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
	if v.dualImg != nil {
		return
	}
	if v.RenderMode == ascimage.ModeBackgroundOnly {
		v.RenderMode = ascimage.ModeQuadrant
	} else {
		v.RenderMode = ascimage.ModeBackgroundOnly
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
	if v.dualImg != nil {
		return
	}
	v.ViewMode = ViewCustom
	v.ZoomLevel += delta
	if v.ZoomLevel < 10 {
		v.ZoomLevel = 10
	}
	if v.ZoomLevel > 400 {
		v.ZoomLevel = 400
	}
}

// Render draws the image into the screen's region
func (v *Viewer) Render(buf tui.Region, termW, termH int) {
	if v.converted == nil {
		return
	}

	availH := termH
	if v.ShowStatus {
		availH--
	}

	offsetX := 0
	offsetY := 0
	if v.converted.Width < termW {
		offsetX = (termW - v.converted.Width) / 2
	}
	if v.converted.Height < availH {
		offsetY = (availH - v.converted.Height) / 2
	}

	for y := range availH {
		srcY := y + v.ViewportY - offsetY
		if srcY < 0 || srcY >= v.converted.Height {
			continue
		}

		for x := range termW {
			srcX := x + v.ViewportX - offsetX
			if srcX < 0 || srcX >= v.converted.Width {
				continue
			}

			srcIdx := srcY*v.converted.Width + srcX
			cell := v.converted.Cells[srcIdx]

			r := cell.Rune
			if r == 0 {
				r = ' '
			}
			buf.CellOpaque(x, y, r, cell.Fg, cell.Bg, cell.Attrs)
		}
	}

	if v.ShowStatus {
		v.renderStatus(buf, termW, termH)
	}
}

func (v *Viewer) renderStatus(buf tui.Region, termW, termH int) {
	y := termH - 1

	statusBg := lcolor.RGB{R: 40, G: 40, B: 50}
	statusFg := lcolor.RGB{R: 200, G: 200, B: 200}
	keyFg := lcolor.RGB{R: 100, G: 180, B: 255}

	for x := range termW {
		buf.CellOpaque(x, y, ' ', statusFg, statusBg, terminal.AttrNone)
	}

	colorStr := "24bit"
	if v.ColorMode == terminal.ColorMode256 {
		colorStr = "256"
	}

	var status, help string

	if v.dualImg != nil {
		status = fmt.Sprintf(" %dx%d aimg | %s | %s ",
			v.srcWidth, v.srcHeight, v.RenderMode.String(), colorStr)
		if v.dualImg.AnchorX != 0 || v.dualImg.AnchorY != 0 {
			status += fmt.Sprintf("| anchor(%d,%d) ", v.dualImg.AnchorX, v.dualImg.AnchorY)
		}
		if v.converted != nil && (v.converted.Width > termW || v.converted.Height > termH-1) {
			status += fmt.Sprintf("| [%d,%d] ", v.ViewportX, v.ViewportY)
		}
		help = " q:quit c:color arrows:pan"
	} else {
		viewStr := "Fit"
		if v.ViewMode == ViewActual {
			viewStr = "1:1"
		} else if v.ViewMode == ViewCustom {
			viewStr = fmt.Sprintf("%d%%", v.ZoomLevel)
		}

		var convW, convH int
		if v.converted != nil {
			convW, convH = v.converted.Width, v.converted.Height
		}

		status = fmt.Sprintf(" %dx%d → %dx%d | %s | %s | %s ",
			v.srcWidth, v.srcHeight, convW, convH,
			v.RenderMode.String(), colorStr, viewStr)
		if v.converted != nil && (v.converted.Width > termW || v.converted.Height > termH-1) {
			status += fmt.Sprintf("| [%d,%d] ", v.ViewportX, v.ViewportY)
		}
		help = " q:quit f:fit m:mode c:color ±:zoom arrows:pan"
	}

	x := 0
	for _, r := range status {
		if x >= termW {
			break
		}
		buf.Cell(x, y, r, statusFg, lcolor.RGB{}, terminal.AttrNone)
		x++
	}

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
			buf.Cell(x, y, r, fg, lcolor.RGB{}, terminal.AttrNone)
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

func runViewer(viewer *Viewer, colorMode terminal.ColorMode) error {
	term := terminal.New(colorMode)

	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal init: %w", err)
	}
	defer term.Fini()

	termW, termH := term.Size()
	buf := make([]terminal.Cell, termW*termH)

	viewer.Update(termW, termH)
	renderFrame(viewer, buf, term, termW, termH)

	for {
		ev := term.PollEvent()

		switch ev.Type {
		case terminal.EventKey:
			action := handleKey(ev, viewer, termW, termH)
			if action == actionQuit {
				return nil
			}
			if action == actionRedraw {
				viewer.ForceUpdate(termW, termH)
			}

		case terminal.EventResize:
			termW, termH = ev.Width, ev.Height
			buf = make([]terminal.Cell, termW*termH)
			viewer.Update(termW, termH)

		case terminal.EventError, terminal.EventClosed:
			return nil
		}

		renderFrame(viewer, buf, term, termW, termH)
	}
}

type keyAction int

const (
	actionNone keyAction = iota
	actionQuit
	actionRedraw
)

func handleKey(ev terminal.Event, viewer *Viewer, termW, termH int) keyAction {
	smallStep := 1
	largeStep := 10
	pageStep := termH / 2

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyCtrlC, terminal.KeyCtrlD:
		return actionQuit
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q', 'Q':
			return actionQuit
		case 'f', 'F':
			viewer.ToggleViewMode()
			return actionRedraw
		case 'm', 'M':
			viewer.ToggleRenderMode()
			return actionRedraw
		case 'c', 'C':
			viewer.ToggleColorMode()
			return actionRedraw
		case 's', 'S':
			viewer.ShowStatus = !viewer.ShowStatus
			return actionNone
		case '+', '=':
			viewer.AdjustZoom(10)
			return actionRedraw
		case '-', '_':
			viewer.AdjustZoom(-10)
			return actionRedraw
		case '0':
			viewer.ZoomLevel = 100
			viewer.ViewMode = ViewCustom
			return actionRedraw
		case 'h':
			viewer.Pan(-smallStep, 0, termW, termH)
		case 'l':
			viewer.Pan(smallStep, 0, termW, termH)
		case 'j':
			viewer.Pan(0, smallStep, termW, termH)
		case 'k':
			viewer.Pan(0, -smallStep, termW, termH)
		case 'H':
			viewer.Pan(-largeStep, 0, termW, termH)
		case 'L':
			viewer.Pan(largeStep, 0, termW, termH)
		case 'J':
			viewer.Pan(0, largeStep, termW, termH)
		case 'K':
			viewer.Pan(0, -largeStep, termW, termH)
		case 'g':
			viewer.PanTo(0, 0, termW, termH)
		case 'G':
			viewer.PanTo(0, 999999, termW, termH)
		}

	case terminal.KeyLeft:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(-step, 0, termW, termH)
	case terminal.KeyRight:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(step, 0, termW, termH)
	case terminal.KeyUp:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(0, -step, termW, termH)
	case terminal.KeyDown:
		step := smallStep
		if ev.Modifiers&terminal.ModShift != 0 {
			step = largeStep
		}
		viewer.Pan(0, step, termW, termH)
	case terminal.KeyPageUp:
		viewer.Pan(0, -pageStep, termW, termH)
	case terminal.KeyPageDown:
		viewer.Pan(0, pageStep, termW, termH)
	case terminal.KeyHome:
		viewer.PanTo(0, viewer.ViewportY, termW, termH)
	case terminal.KeyEnd:
		viewer.PanTo(999999, viewer.ViewportY, termW, termH)
	}

	return actionNone
}

// renderFrame draws a frame on the backdrop a cell nothing drew shows
func renderFrame(viewer *Viewer, buf []terminal.Cell, term terminal.Terminal, termW, termH int) {
	root := tui.NewRegion(buf, termW, 0, 0, termW, termH)
	root.Fill(lcolor.Gunmetal)
	viewer.Render(root, termW, termH)
	term.Flush(buf, termW, termH)
}
