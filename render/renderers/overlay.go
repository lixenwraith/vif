package renderers

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
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

// cardLayout holds calculated position and size for a card
type cardLayout struct {
	x, y, w, h int
	card       core.OverlayCard
}

// OverlayRenderer draws the modal overlay window
type OverlayRenderer struct {
	gameCtx *engine.GameContext
	adapter *TUIAdapter
}

// NewOverlayRenderer creates a new overlay renderer
func NewOverlayRenderer(gameCtx *engine.GameContext) *OverlayRenderer {
	return &OverlayRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws the overlay window using TUI primitives
func (r *OverlayRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Calculate overlay dimensions
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

	// Ensure adapter sized
	if r.adapter == nil || r.adapter.Width() != overlayW || r.adapter.Height() != overlayH {
		r.adapter = NewTUIAdapter(overlayW, overlayH)
	}

	// Clear adapter for fresh frame
	r.adapter.Clear(render.RgbOverlayBg)

	root := r.adapter.Region()
	content := r.gameCtx.GetOverlayContent()

	title := ""
	if content != nil {
		title = content.Title
	}

	result := root.Overlay(tui.OverlayOpts{
		Style:   tui.OverlayBorderTitle,
		Title:   title,
		Border:  tui.LineDouble,
		Bg:      render.RgbOverlayBg,
		Fg:      render.RgbOverlayBorder,
		TitleFg: render.RgbOverlayTitle,
	})

	if content != nil {
		r.renderTypedContent(root, result.Content, content)
	}

	r.adapter.FlushTo(buf, startX, startY, constant.MaskUI)
}

// IsVisible implements render.VisibilityToggle
func (r *OverlayRenderer) IsVisible() bool {
	return r.gameCtx.IsOverlayActive()
}

func (r *OverlayRenderer) renderTypedContent(outer, inner tui.Region, content *core.OverlayContent) {
	contentRegion := inner.Sub(
		constant.OverlayPaddingX,
		constant.OverlayPaddingY,
		inner.W-2*constant.OverlayPaddingX,
		inner.H-2*constant.OverlayPaddingY-1,
	)
	contentW := contentRegion.W
	contentH := contentRegion.H

	// Get cards and calculate layout
	cards := content.Cards()
	if len(cards) == 0 {
		return
	}

	layouts := r.calculateCardLayouts(cards, contentW, contentH)
	scroll := r.gameCtx.GetOverlayScroll()

	// Calculate total content height for scroll
	totalH := 0
	for _, l := range layouts {
		if l.y+l.h > totalH {
			totalH = l.y + l.h
		}
	}

	// Clamp scroll
	maxScroll := totalH - contentH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	// Render visible cards
	for _, l := range layouts {
		cardY := l.y - scroll
		// Skip if entirely above viewport
		if cardY+l.h < 0 {
			continue
		}
		// Skip if entirely below viewport
		if cardY >= contentH {
			continue
		}

		// Clip card region to viewport
		visibleY := cardY
		visibleH := l.h
		entryOffset := 0

		if cardY < 0 {
			entryOffset = -cardY
			visibleH += cardY
			visibleY = 0
		}
		if visibleY+visibleH > contentH {
			visibleH = contentH - visibleY
		}

		if visibleH > 0 {
			cardRegion := contentRegion.Sub(l.x, visibleY, l.w, visibleH)
			r.renderCard(cardRegion, l.card, entryOffset, visibleH)
		}
	}

	// Navigation hints
	hints := "ESC close · j/k scroll · PgUp/PgDn page"
	hintsX := (outer.W - tui.RuneLen(hints)) / 2
	outer.Text(hintsX, outer.H-2, hints, render.RgbOverlayHint, render.RgbOverlayBg, terminal.AttrDim)

	// Scroll indicator
	if totalH > contentH {
		indicator := fmt.Sprintf("[%d/%d]", scroll+1, maxScroll+1)
		indX := outer.W - tui.RuneLen(indicator) - 1
		outer.Text(indX, outer.H-1, indicator, render.RgbOverlayBorder, render.RgbOverlayBg, terminal.AttrNone)
	}
}

func (r *OverlayRenderer) calculateCardLayouts(cards []core.OverlayCard, availW, availH int) []cardLayout {
	// Determine column count based on width
	var cols int
	switch {
	case availW >= 140:
		cols = 4
	case availW >= 100:
		cols = 3
	case availW >= 60:
		cols = 2
	default:
		cols = 1
	}

	gap := 2
	colW := (availW - (cols-1)*gap) / cols

	layouts := make([]cardLayout, 0, len(cards))
	colHeights := make([]int, cols) // Track height used in each column

	for _, card := range cards {
		// Card height: 2 (border) + entries
		cardH := 2 + len(card.Entries)
		if cardH < 3 {
			cardH = 3
		}

		// Find shortest column
		minCol := 0
		minH := colHeights[0]
		for i := 1; i < cols; i++ {
			if colHeights[i] < minH {
				minH = colHeights[i]
				minCol = i
			}
		}

		x := minCol * (colW + gap)
		y := colHeights[minCol]

		layouts = append(layouts, cardLayout{
			x: x, y: y, w: colW, h: cardH,
			card: card,
		})

		colHeights[minCol] += cardH + 1 // +1 for gap between cards
	}

	return layouts
}

func (r *OverlayRenderer) renderCard(region tui.Region, card core.OverlayCard, entryOffset, visibleH int) {
	// Draw card frame if top border visible
	if entryOffset == 0 {
		region.Box(tui.LineSingle, render.RgbOverlayBorder)

		// Title in top border
		if card.Title != "" && region.W > 4 {
			title := " " + card.Title + " "
			if tui.RuneLen(title) > region.W-4 {
				title = tui.Truncate(title, region.W-4)
			}
			titleX := 2
			region.Text(titleX, 0, title, render.RgbOverlayHeader, render.RgbOverlayBg, terminal.AttrBold)
		}
	}

	// Content area inside card
	innerX := 1
	innerY := 1 - entryOffset
	innerW := region.W - 2
	innerH := region.H

	if innerW < 1 {
		return
	}

	keyStyle := tui.Style{Fg: render.RgbOverlayKey, Bg: render.RgbOverlayBg}
	valStyle := tui.Style{Fg: render.RgbOverlayValue, Bg: render.RgbOverlayBg}

	for i, entry := range card.Entries {
		y := innerY + i
		if y < 0 {
			continue
		}
		if y >= innerH-1 { // -1 for bottom border
			break
		}

		inner := region.Sub(innerX, y, innerW, 1)
		inner.KeyValue(0, entry.Key, entry.Value, keyStyle, valStyle, ':')
	}

	// Draw bottom border if visible
	bottomY := 1 + len(card.Entries) - entryOffset
	if bottomY >= 0 && bottomY < region.H {
		for x := 1; x < region.W-1; x++ {
			region.Cell(x, bottomY, '─', render.RgbOverlayBorder, render.RgbOverlayBg, terminal.AttrNone)
		}
		region.Cell(0, bottomY, '└', render.RgbOverlayBorder, render.RgbOverlayBg, terminal.AttrNone)
		region.Cell(region.W-1, bottomY, '┘', render.RgbOverlayBorder, render.RgbOverlayBg, terminal.AttrNone)
	}
}