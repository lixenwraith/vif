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

// OverlayRenderer draws the modal overlay window
type OverlayRenderer struct {
	gameCtx *engine.GameContext
	adapter *TUIAdapter
}

// overlaySection represents parsed content section
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

// Render draws the overlay window using TUI primitives
func (r *OverlayRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	uiSnapshot := r.gameCtx.GetUISnapshot()

	// Calculate overlay dimensions (80% of screen)
	overlayW := int(float64(ctx.Width) * constant.OverlayWidthPercent)
	overlayH := int(float64(ctx.Height) * constant.OverlayHeightPercent)

	if overlayW < 40 {
		overlayW = 40
	}
	if overlayH < 15 {
		overlayH = 15
	}

	startX := (ctx.Width - overlayW) / 2
	startY := (ctx.Height - overlayH) / 2

	// Ensure adapter is correctly sized
	if r.adapter == nil || r.adapter.Width() != overlayW || r.adapter.Height() != overlayH {
		r.adapter = NewTUIAdapter(overlayW, overlayH)
	}

	// Get root region and draw frame
	root := r.adapter.Region()
	root.BoxFilled(tui.LineDouble, render.RgbOverlayBorder, render.RgbOverlayBg)

	// Draw title in top border
	r.drawTitle(root, uiSnapshot.OverlayTitle, overlayW)

	// Content area inside border with padding
	contentX := 1 + constant.OverlayPaddingX
	contentY := 1 + constant.OverlayPaddingY
	contentW := overlayW - 2 - (2 * constant.OverlayPaddingX)
	contentH := overlayH - 2 - (2 * constant.OverlayPaddingY) - 1 // -1 for hints row

	content := root.Sub(contentX, contentY, contentW, contentH)

	// Parse content sections
	sections := r.parseSections(uiSnapshot.OverlayContent)

	// Calculate total lines for scroll bounds
	totalLines := r.countTotalLines(sections)
	scroll := uiSnapshot.OverlayScroll
	if scroll > totalLines-contentH {
		scroll = totalLines - contentH
	}
	if scroll < 0 {
		scroll = 0
	}

	// Render sections with two-column layout
	r.renderSections(content, sections, scroll, contentH)

	// Render hints at bottom
	hintsRegion := root.Sub(1, overlayH-2, overlayW-2, 1)
	r.renderHints(hintsRegion, sections)

	// Render scroll indicator in bottom-right of border
	if totalLines > contentH {
		r.renderScrollIndicator(root, overlayW, overlayH, scroll, contentH, totalLines)
	}

	// Flush adapter to main render buffer
	r.adapter.FlushTo(buf, startX, startY, render.MaskUI)
}

func (r *OverlayRenderer) drawTitle(root tui.Region, title string, width int) {
	if title == "" {
		return
	}
	displayTitle := fmt.Sprintf(" %s ", strings.ToUpper(title))
	titleX := (width - tui.RuneLen(displayTitle)) / 2
	root.Text(titleX, 0, displayTitle, render.RgbOverlayTitle, render.RgbOverlayBg, terminal.AttrBold)
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

func (r *OverlayRenderer) renderSections(content tui.Region, sections []overlaySection, scroll, maxLines int) {
	// Filter out hints
	var dataSections []overlaySection
	for _, sec := range sections {
		if !sec.isHint {
			dataSections = append(dataSections, sec)
		}
	}

	if len(dataSections) == 0 {
		return
	}

	// Distribute sections into two columns
	leftSections, rightSections := r.distributeSections(dataSections)

	// Calculate column widths
	colGap := 3
	colWidth := (content.W - colGap) / 2
	if len(rightSections) == 0 {
		// Single column centered
		colWidth = content.W * 2 / 3
		if colWidth > 60 {
			colWidth = 60
		}
		leftX := (content.W - colWidth) / 2
		leftCol := content.Sub(leftX, 0, colWidth, content.H)
		r.renderColumn(leftCol, leftSections, scroll, maxLines)
	} else {
		// Two columns
		leftCol := content.Sub(0, 0, colWidth, content.H)
		rightCol := content.Sub(colWidth+colGap, 0, colWidth, content.H)
		r.renderColumn(leftCol, leftSections, scroll, maxLines)
		r.renderColumn(rightCol, rightSections, scroll, maxLines)
	}
}

func (r *OverlayRenderer) distributeSections(sections []overlaySection) (left, right []overlaySection) {
	leftLines, rightLines := 0, 0
	for _, sec := range sections {
		cost := 2 + len(sec.items) // header + spacer + items
		if leftLines <= rightLines {
			left = append(left, sec)
			leftLines += cost
		} else {
			right = append(right, sec)
			rightLines += cost
		}
	}
	return
}

func (r *OverlayRenderer) renderColumn(col tui.Region, sections []overlaySection, scroll, maxLines int) {
	y := 0
	lineNum := 0

	keyStyle := tui.Style{Fg: render.RgbOverlayKey, Bg: render.RgbOverlayBg}
	valStyle := tui.Style{Fg: render.RgbOverlayValue, Bg: render.RgbOverlayBg}

	for _, sec := range sections {
		// Section header
		if lineNum >= scroll && y < col.H {
			col.Divider(y, sec.header, tui.LineSingle, render.RgbOverlayHeader)
			y++
		}
		lineNum++

		// Items
		for _, item := range sec.items {
			if lineNum >= scroll && y < col.H {
				col.KeyValue(y, item[0], item[1], keyStyle, valStyle, ':')
				y++
			}
			lineNum++
		}

		// Spacer between sections
		if lineNum >= scroll && y < col.H {
			y++
		}
		lineNum++
	}
}

func (r *OverlayRenderer) renderHints(region tui.Region, sections []overlaySection) {
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
	if tui.RuneLen(combined) > region.W {
		combined = tui.Truncate(combined, region.W)
	}

	region.TextCenter(0, combined, render.RgbOverlayHint, render.RgbOverlayBg, terminal.AttrDim)
}

func (r *OverlayRenderer) renderScrollIndicator(root tui.Region, w, h, scroll, visible, total int) {
	indicator := fmt.Sprintf("[%d/%d]", scroll+1, total-visible+1)
	x := w - tui.RuneLen(indicator) - 1
	root.Text(x, h-1, indicator, render.RgbOverlayBorder, render.RgbOverlayBg, terminal.AttrNone)
}

func (r *OverlayRenderer) countTotalLines(sections []overlaySection) int {
	leftLines, rightLines := 0, 0
	for _, sec := range sections {
		if sec.isHint {
			continue
		}
		cost := 2 + len(sec.items)
		if leftLines <= rightLines {
			leftLines += cost
		} else {
			rightLines += cost
		}
	}
	if leftLines > rightLines {
		return leftLines
	}
	return rightLines
}