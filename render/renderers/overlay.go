package renderers

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// OverlayRenderer draws the modal overlay window
type OverlayRenderer struct {
	gameCtx *engine.GameContext
	adapter *TUIAdapter
}

type overlaySection struct {
	header string
	items  [][2]string
	isHint bool
}

// NewOverlayRenderer creates a new overlay renderer
func NewOverlayRenderer(gameCtx *engine.GameContext) *OverlayRenderer {
	return &OverlayRenderer{
		gameCtx: gameCtx,
	}
}

// IsVisible returns true when the overlay should be rendered
func (r *OverlayRenderer) IsVisible() bool {
	uiSnapshot := r.gameCtx.GetUISnapshot()
	return r.gameCtx.IsOverlayMode() && uiSnapshot.OverlayActive
}

// Render draws the overlay window with a colored two-column section layout
func (r *OverlayRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(render.MaskUI)
	uiSnapshot := r.gameCtx.GetUISnapshot()

	// Calculate overlay dimensions (80% of screen)
	overlayWidth := int(float64(ctx.Width) * constant.OverlayWidthPercent)
	overlayHeight := int(float64(ctx.Height) * constant.OverlayHeightPercent)

	if overlayWidth < 40 {
		overlayWidth = 40
	}
	if overlayHeight < 15 {
		overlayHeight = 15
	}

	startX := (ctx.Width - overlayWidth) / 2
	startY := (ctx.Height - overlayHeight) / 2

	// 1. Draw Background and Borders
	for y := 0; y < overlayHeight; y++ {
		for x := 0; x < overlayWidth; x++ {
			buf.SetWithBg(startX+x, startY+y, ' ', render.RgbOverlayText, render.RgbOverlayBg)
		}
	}

	r.drawBorder(buf, startX, startY, overlayWidth, overlayHeight, uiSnapshot.OverlayTitle)

	// 2. Content Layout Calculation
	contentStartX := startX + 1 + constant.OverlayPaddingX
	contentStartY := startY + 1 + constant.OverlayPaddingY
	contentWidth := overlayWidth - 2 - (2 * constant.OverlayPaddingX)
	contentHeight := overlayHeight - 2 - (2 * constant.OverlayPaddingY)

	sections := r.parseSections(uiSnapshot.OverlayContent)
	leftSections, rightSections := r.distributeSections(sections)

	maxLines := contentHeight - 1

	// 3. Render Columns - center if single column
	if len(rightSections) == 0 {
		// Single column: center it
		colWidth := (contentWidth * 2) / 3
		if colWidth > 60 {
			colWidth = 60
		}
		centerX := contentStartX + (contentWidth-colWidth)/2
		r.renderColumn(buf, leftSections, centerX, contentStartY, colWidth, maxLines, uiSnapshot.OverlayScroll)
	} else {
		// Two columns
		colWidth := (contentWidth - 3) / 2
		leftColX := contentStartX
		rightColX := contentStartX + colWidth + 3
		r.renderColumn(buf, leftSections, leftColX, contentStartY, colWidth, maxLines, uiSnapshot.OverlayScroll)
		r.renderColumn(buf, rightSections, rightColX, contentStartY, colWidth, maxLines, uiSnapshot.OverlayScroll)
	}

	// 4. Render Hints (Bottom Center)
	r.renderHints(buf, sections, startX, startY+overlayHeight-2, overlayWidth)

	// 5. Scroll Indicator
	totalLines := r.countTotalLines(sections)
	if totalLines > maxLines {
		scrollInfo := fmt.Sprintf("[%d/%d]", uiSnapshot.OverlayScroll+1, totalLines-maxLines+1)
		scrollX := startX + overlayWidth - len(scrollInfo) - 2
		scrollY := startY + overlayHeight - 1
		for i, ch := range scrollInfo {
			buf.SetWithBg(scrollX+i, scrollY, ch, render.RgbOverlayBorder, render.RgbOverlayBg)
		}
	}
}

func (r *OverlayRenderer) drawBorder(buf *render.RenderBuffer, x, y, w, h int, title string) {
	// Corners
	buf.SetWithBg(x, y, '╔', render.RgbOverlayBorder, render.RgbOverlayBg)
	buf.SetWithBg(x+w-1, y, '╗', render.RgbOverlayBorder, render.RgbOverlayBg)
	buf.SetWithBg(x, y+h-1, '╚', render.RgbOverlayBorder, render.RgbOverlayBg)
	buf.SetWithBg(x+w-1, y+h-1, '╝', render.RgbOverlayBorder, render.RgbOverlayBg)

	// Horizontal lines
	for i := 1; i < w-1; i++ {
		buf.SetWithBg(x+i, y, '═', render.RgbOverlayBorder, render.RgbOverlayBg)
		buf.SetWithBg(x+i, y+h-1, '═', render.RgbOverlayBorder, render.RgbOverlayBg)
	}

	// Vertical lines
	for i := 1; i < h-1; i++ {
		buf.SetWithBg(x, y+i, '║', render.RgbOverlayBorder, render.RgbOverlayBg)
		buf.SetWithBg(x+w-1, y+i, '║', render.RgbOverlayBorder, render.RgbOverlayBg)
	}

	// Title
	if title != "" {
		displayTitle := fmt.Sprintf(" %s ", strings.ToUpper(title))
		titleX := x + (w-len(displayTitle))/2
		for i, ch := range displayTitle {
			buf.SetWithBg(titleX+i, y, ch, render.RgbOverlayTitle, render.RgbOverlayBg)
		}
	}
}

func (r *OverlayRenderer) parseSections(content []string) []overlaySection {
	var sections []overlaySection
	var current *overlaySection

	for _, line := range content {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '§':
			if current != nil {
				sections = append(sections, *current)
			}
			current = &overlaySection{header: line[1:]}
		case '~':
			if current != nil {
				sections = append(sections, *current)
			}
			sections = append(sections, overlaySection{header: line[1:], isHint: true})
			current = nil
		default:
			if current == nil {
				current = &overlaySection{header: "INFO"}
			}
			if idx := strings.Index(line, "|"); idx > 0 {
				current.items = append(current.items, [2]string{line[:idx], line[idx+1:]})
			} else {
				current.items = append(current.items, [2]string{line, ""})
			}
		}
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

func (r *OverlayRenderer) distributeSections(sections []overlaySection) (left, right []overlaySection) {
	lLines, rLines := 0, 0
	for _, sec := range sections {
		if sec.isHint {
			continue
		}
		cost := 1 + len(sec.items) + 1
		if lLines <= rLines {
			left = append(left, sec)
			lLines += cost
		} else {
			right = append(right, sec)
			rLines += cost
		}
	}
	return
}

func (r *OverlayRenderer) renderColumn(buf *render.RenderBuffer, sections []overlaySection, x, y, width, maxLines, scroll int) {
	lineY := y
	currentLine := 0
	maxY := y + maxLines

	for _, sec := range sections {
		// Header
		if currentLine >= scroll && lineY < maxY {
			r.drawSectionHeader(buf, sec.header, x, lineY, width)
			lineY++
		}
		currentLine++

		// Items
		for _, item := range sec.items {
			if currentLine >= scroll && lineY < maxY {
				r.drawKeyValue(buf, item[0], item[1], x, lineY, width)
				lineY++
			}
			currentLine++
		}

		// Spacer
		if currentLine >= scroll && lineY < maxY {
			lineY++
		}
		currentLine++
	}
}

func (r *OverlayRenderer) drawSectionHeader(buf *render.RenderBuffer, text string, x, y, width int) {
	// Draw horizontal line with embedded text: ┌─ TITLE ──┐
	header := fmt.Sprintf(" %s ", text)
	buf.SetWithBg(x, y, '┌', render.RgbOverlaySeparator, render.RgbOverlayBg)
	for i := 1; i < width-1; i++ {
		buf.SetWithBg(x+i, y, '─', render.RgbOverlaySeparator, render.RgbOverlayBg)
	}
	buf.SetWithBg(x+width-1, y, '┐', render.RgbOverlaySeparator, render.RgbOverlayBg)

	for i, ch := range header {
		if i+2 < width-1 {
			buf.SetWithBg(x+2+i, y, ch, render.RgbOverlayHeader, render.RgbOverlayBg)
		}
	}
}

func (r *OverlayRenderer) drawKeyValue(buf *render.RenderBuffer, key, val string, x, y, width int) {
	keyWidth := (width / 2) - 1
	if len(key) > keyWidth {
		key = key[:keyWidth-1] + "…"
	}

	// Key (right aligned in first half)
	for i, ch := range key {
		buf.SetWithBg(x+keyWidth-len(key)+i, y, ch, render.RgbOverlayKey, render.RgbOverlayBg)
	}

	buf.SetWithBg(x+keyWidth, y, ':', render.RgbOverlaySeparator, render.RgbOverlayBg)

	// Value (left aligned in second half)
	valArea := width - keyWidth - 2
	if len(val) > valArea {
		val = val[:valArea-1] + "…"
	}
	for i, ch := range val {
		buf.SetWithBg(x+keyWidth+2+i, y, ch, render.RgbOverlayValue, render.RgbOverlayBg)
	}
}

func (r *OverlayRenderer) renderHints(buf *render.RenderBuffer, sections []overlaySection, x, y, w int) {
	var hints []string
	for _, sec := range sections {
		if sec.isHint {
			hints = append(hints, sec.header)
		}
	}
	if len(hints) == 0 {
		return
	}

	combined := strings.Join(hints, "  •  ")
	if len(combined) > w-4 {
		combined = combined[:w-7] + "..."
	}

	startX := x + (w-len(combined))/2
	for i, ch := range combined {
		buf.SetWithBg(startX+i, y, ch, render.RgbOverlayHint, render.RgbOverlayBg)
	}
}

func (r *OverlayRenderer) countTotalLines(sections []overlaySection) int {
	l, rLines := 0, 0
	for _, sec := range sections {
		if sec.isHint {
			continue
		}
		cost := 1 + len(sec.items) + 1
		if l <= rLines {
			l += cost
		} else {
			rLines += cost
		}
	}
	if l > rLines {
		return l
	}
	return rLines
}

// TUIAdapter bridges terminal/tui to render.RenderBuffer
type TUIAdapter struct {
	cells  []terminal.Cell
	width  int
	height int
}

// NewTUIAdapter creates adapter with given dimensions
func NewTUIAdapter(width, height int) *TUIAdapter {
	size := width * height
	cells := make([]terminal.Cell, size)
	return &TUIAdapter{
		cells:  cells,
		width:  width,
		height: height,
	}
}

// Resize adjusts adapter dimensions, reallocating if needed
func (a *TUIAdapter) Resize(width, height int) {
	size := width * height
	if cap(a.cells) < size {
		a.cells = make([]terminal.Cell, size)
	} else {
		a.cells = a.cells[:size]
	}
	a.width = width
	a.height = height
}

// Region returns a tui.Region covering the entire adapter buffer
func (a *TUIAdapter) Region() tui.Region {
	return tui.NewRegion(a.cells, a.width, 0, 0, a.width, a.height)
}

// SubRegion returns a tui.Region for a portion of the buffer
func (a *TUIAdapter) SubRegion(x, y, w, h int) tui.Region {
	return tui.NewRegion(a.cells, a.width, x, y, w, h)
}

// Clear fills buffer with specified background
func (a *TUIAdapter) Clear(bg terminal.RGB) {
	for i := range a.cells {
		a.cells[i] = terminal.Cell{Rune: ' ', Bg: bg}
	}
}

// FlushTo copies adapter buffer to RenderBuffer at offset with mask
func (a *TUIAdapter) FlushTo(buf *render.RenderBuffer, offsetX, offsetY int, mask uint8) {
	buf.SetWriteMask(mask)
	for y := 0; y < a.height; y++ {
		for x := 0; x < a.width; x++ {
			idx := y*a.width + x
			cell := a.cells[idx]
			ch := cell.Rune
			if ch == 0 {
				ch = ' '
			}
			buf.SetWithBg(offsetX+x, offsetY+y, ch, cell.Fg, cell.Bg)
		}
	}
}

// Width returns adapter width
func (a *TUIAdapter) Width() int {
	return a.width
}

// Height returns adapter height
func (a *TUIAdapter) Height() int {
	return a.height
}