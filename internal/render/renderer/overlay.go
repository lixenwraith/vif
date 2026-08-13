package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
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
func (a *TUIAdapter) Clear(bg color.RGB) {
	for i := range a.cells {
		a.cells[i] = terminal.Cell{Rune: ' ', Bg: bg}
	}
}

// FlushTo copies adapter buffer to RenderBuffer at offset with mask
func (a *TUIAdapter) FlushTo(buf *render.RenderBuffer, offsetX, offsetY int, mask uint8) {
	buf.SetWriteMask(mask)
	for y := range a.height {
		for x := range a.width {
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

// overlayCardBreakpoints maps content width to masonry column count
var overlayCardBreakpoints = map[int]int{
	parameter.OverlayCardCols4: 4,
	parameter.OverlayCardCols3: 3,
	parameter.OverlayCardCols2: 2,
}

// OverlayRenderer draws the modal overlay window
type OverlayRenderer struct {
	gameCtx *engine.GameContext
	adapter *TUIAdapter

	// Layout cache, rebuilt when the content or the content viewport changes
	content *core.OverlayContent
	cards   []core.OverlayCard
	items   []tui.MasonryItem
	blocks  []tui.DocBlock
	masonry *tui.MasonryState
	doc     *tui.DocState
	layoutW int
	layoutH int
	selKey  string // Last selection scrolled into view
}

// NewOverlayRenderer creates a new overlay renderer
func NewOverlayRenderer(gameCtx *engine.GameContext) *OverlayRenderer {
	return &OverlayRenderer{
		gameCtx: gameCtx,
	}
}

// IsVisible implements render.VisibilityToggle
func (r *OverlayRenderer) IsVisible() bool {
	return r.gameCtx.IsOverlayActive()
}

// Render draws the overlay window using TUI primitives
func (r *OverlayRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	g := r.gameCtx.OverlayGeometry()
	if !g.Valid {
		return // Terminal too small to draw a legible window
	}

	if r.adapter == nil {
		r.adapter = NewTUIAdapter(g.W, g.H)
	} else if r.adapter.Width() != g.W || r.adapter.Height() != g.H {
		r.adapter.Resize(g.W, g.H)
	}
	r.adapter.Clear(visual.RgbOverlayBg)

	root := r.adapter.Region()
	content := r.gameCtx.GetOverlayContent()

	title := ""
	if content != nil {
		title = content.Title
	}

	root.Overlay(tui.OverlayOpts{
		Style:   tui.OverlayBorderTitle,
		Title:   title,
		Border:  tui.LineDouble,
		Bg:      visual.RgbOverlayBg,
		Fg:      visual.RgbOverlayBorder,
		TitleFg: visual.RgbOverlayTitle,
	})

	if content != nil {
		r.renderContent(root, g, content)
	}

	r.adapter.FlushTo(buf, g.X, g.Y, visual.MaskUI)
}

// renderContent dispatches on the content's layout and draws the chrome
func (r *OverlayRenderer) renderContent(root tui.Region, g engine.OverlayGeometry, data *core.OverlayContent) {
	r.syncLayout(g, data)

	body := root.Sub(g.ContentX, g.ContentY, g.ContentW, g.ContentH)

	switch data.Layout {
	case core.OverlayLayoutAbout:
		r.renderAbout(body)
		r.renderHint(root, g, parameter.OverlayHintsAbout)
		return

	case core.OverlayLayoutDoc:
		r.renderDoc(body)
		r.renderScrollBar(root, g, r.doc.Viewport)
		r.renderHint(root, g, parameter.OverlayHintsDoc)

	default:
		r.renderCards(body)
		if r.masonry != nil {
			r.renderScrollBar(root, g, r.masonry.Viewport)
		}
		r.renderHint(root, g, parameter.OverlayHintsCards)
	}
}

// syncLayout rebuilds the cached layout when the content or viewport changes
func (r *OverlayRenderer) syncLayout(g engine.OverlayGeometry, data *core.OverlayContent) {
	if r.content == data && r.layoutW == g.ContentW && r.layoutH == g.ContentH {
		return
	}
	r.content, r.layoutW, r.layoutH = data, g.ContentW, g.ContentH
	r.cards = data.Cards()

	switch data.Layout {
	case core.OverlayLayoutAbout:
		r.gameCtx.SetOverlayContentH(0)
	case core.OverlayLayoutDoc:
		r.buildDoc(g)
	default:
		r.buildCards(g)
	}
}

// buildCards computes the masonry layout and publishes the card index
func (r *OverlayRenderer) buildCards(g engine.OverlayGeometry) {
	if r.masonry == nil {
		r.masonry = tui.NewMasonryState()
	}

	r.items = r.items[:0]
	for i := range r.cards {
		r.items = append(r.items, tui.MasonryItem{
			Key:    r.cards[i].Key,
			Height: parameter.OverlayCardFrameRows + len(r.cards[i].Entries),
			Data:   &r.cards[i],
		})
	}

	r.masonry.CalculateLayout(r.items, g.ContentW, tui.MasonryOpts{
		GapX:        parameter.OverlayCardGapX,
		GapY:        parameter.OverlayCardGapY,
		Breakpoints: overlayCardBreakpoints,
	})
	r.masonry.SetViewport(g.ContentH)
	r.gameCtx.SetOverlayContentH(r.masonry.Viewport.ContentH)

	// Input resolves selection against this index; rebuilt only on relayout
	refs := make([]engine.OverlayCardRef, 0, len(r.masonry.Layouts))
	for i := range r.masonry.Layouts {
		l := &r.masonry.Layouts[i]
		refs = append(refs, engine.OverlayCardRef{Key: l.Item.Key, X: l.X, Y: l.Y, W: l.W, H: l.H})
	}
	r.gameCtx.SetOverlayCards(refs)

	r.selKey = "" // Force the selection back into view after a relayout
}

// buildDoc flattens cards into document blocks and publishes the row count
func (r *OverlayRenderer) buildDoc(g engine.OverlayGeometry) {
	if r.doc == nil {
		r.doc = tui.NewDocState()
	}

	r.blocks = r.blocks[:0]
	for i := range r.cards {
		r.blocks = append(r.blocks, tui.DocBlock{Kind: tui.DocSection, Text: r.cards[i].Title})
		for _, e := range r.cards[i].Entries {
			r.blocks = append(r.blocks, tui.DocBlock{Kind: tui.DocEntry, Key: e.Key, Text: e.Value})
		}
	}

	r.doc.Layout(r.blocks, g.ContentW, r.docOpts())
	r.doc.SetViewport(g.ContentH)
	r.gameCtx.SetOverlayContentH(r.doc.Viewport.ContentH)
}

// docOpts returns the document styling; layout-affecting fields match buildDoc
func (r *OverlayRenderer) docOpts() tui.DocOpts {
	bg := visual.RgbOverlayBg
	return tui.DocOpts{
		HeaderStyle: tui.Style{Fg: visual.RgbOverlayHeader, Bg: bg, Attr: terminal.AttrBold},
		KeyStyle:    tui.Style{Fg: visual.RgbOverlayKey, Bg: bg},
		TextStyle:   tui.Style{Fg: visual.RgbOverlayValue, Bg: bg},
		RuleStyle:   tui.Style{Fg: visual.RgbOverlaySeparator, Bg: bg, Attr: terminal.AttrDim},
		Rule:        tui.LineSingle,
		KeyMaxW:     parameter.HelpKeyMaxWidth,
		MinTextW:    parameter.HelpMinTextWidth,
		Gap:         parameter.HelpColumnGap,
		Indent:      parameter.HelpIndent,
		StackIndent: parameter.HelpStackIndent,
		SectionGap:  parameter.HelpSectionGap,
		MaxWidth:    parameter.HelpMaxWidth,
		Center:      true,
	}
}

// renderCards draws the visible masonry slice, keeping the selection in view
func (r *OverlayRenderer) renderCards(body tui.Region) {
	if r.masonry == nil || len(r.cards) == 0 {
		return
	}

	r.masonry.SetViewport(body.H)
	r.masonry.Viewport.ScrollTo(r.gameCtx.GetOverlayScroll())

	// Scroll only when the selection moved, so paging stays free
	sel := r.gameCtx.GetOverlaySelection()
	if sel != r.selKey {
		r.selKey = sel
		if l := r.findLayout(sel); l != nil {
			r.masonry.Viewport.EnsureRange(l.Y, l.H)
		}
	}

	body.Masonry(r.masonry, func(region tui.Region, layout tui.MasonryLayout, contentOffset int) {
		card, ok := layout.Item.Data.(*core.OverlayCard)
		if !ok {
			return
		}
		r.renderCard(region, card, layout.H, contentOffset, card.Key == sel)
	})

	r.gameCtx.SetOverlayScroll(r.masonry.Viewport.Offset)
}

// findLayout returns the layout of a card by key, nil when absent
func (r *OverlayRenderer) findLayout(key string) *tui.MasonryLayout {
	if key == "" {
		return nil
	}
	for i := range r.masonry.Layouts {
		if r.masonry.Layouts[i].Item.Key == key {
			return &r.masonry.Layouts[i]
		}
	}
	return nil
}

// renderCard draws one card slice; totalH is the card's full height and off the
// rows clipped from its top, so the frame stays correct at either fold
func (r *OverlayRenderer) renderCard(region tui.Region, card *core.OverlayCard, totalH, off int, selected bool) {
	line, fg := tui.LineSingle, visual.RgbOverlayBorder
	if card.Pinned {
		line, fg = tui.LineDouble, visual.RgbOverlayPinned
	}
	if selected {
		fg = visual.RgbOverlaySelected
	}
	region.BoxClipped(line, fg, totalH, off)

	// Title sits on the top border row, drawn only when that row is in view
	if off == 0 && card.Title != "" && region.W > 4 {
		title := " " + card.Title + " "
		if card.Pinned {
			title = " " + string(parameter.OverlayPinMarker) + " " + card.Title + " "
		}
		if tui.RuneLen(title) > region.W-4 {
			title = tui.Truncate(title, region.W-4)
		}
		attr := terminal.AttrBold
		if selected {
			attr |= terminal.AttrReverse
		}
		region.Text(2, 0, title, visual.RgbOverlayHeader, visual.RgbOverlayBg, attr)
	}

	innerW := region.W - 2
	if innerW < 1 {
		return
	}

	keyStyle := tui.Style{Fg: visual.RgbOverlayKey, Bg: visual.RgbOverlayBg}
	valStyle := tui.Style{Fg: visual.RgbOverlayValue, Bg: visual.RgbOverlayBg}

	// Entry i occupies card row 1+i; only region bounds gate it
	for i := range card.Entries {
		y := 1 + i - off
		if y < 0 {
			continue
		}
		if y >= region.H {
			break
		}
		row := region.Sub(1, y, innerW, 1)
		row.KeyValue(0, card.Entries[i].Key, card.Entries[i].Value, keyStyle, valStyle, ':')
	}
}

// renderDoc draws the visible document slice and syncs the clamped scroll back
func (r *OverlayRenderer) renderDoc(body tui.Region) {
	if r.doc == nil {
		return
	}
	r.doc.SetViewport(body.H)
	r.doc.Viewport.ScrollTo(r.gameCtx.GetOverlayScroll())
	body.Doc(r.doc, r.docOpts())
	r.gameCtx.SetOverlayScroll(r.doc.Viewport.Offset)
}

// renderScrollBar draws the reserved scroll column, hidden when content fits
func (r *OverlayRenderer) renderScrollBar(root tui.Region, g engine.OverlayGeometry, v *tui.ViewportScroll) {
	if g.ScrollW < 1 || v == nil {
		return
	}
	bar := root.Sub(g.ScrollX, g.ContentY, g.ScrollW, g.ContentH)
	bar.ScrollBarStyled(0, v.Offset, v.ViewportH, v.ContentH, tui.ScrollBarOpts{
		ThumbFg:  visual.RgbOverlayScrollThumb,
		TrackFg:  visual.RgbOverlayScrollTrack,
		Bg:       visual.RgbOverlayBg,
		HideIdle: true,
	})
}

// renderHint draws the widest hint variant that fits, or none at all
func (r *OverlayRenderer) renderHint(root tui.Region, g engine.OverlayGeometry, tiers []string) {
	if g.HintY < 0 {
		return
	}
	avail := g.W - 2
	for _, hint := range tiers {
		n := tui.RuneLen(hint)
		if n > avail {
			continue
		}
		root.Text(1+(avail-n)/2, g.HintY, hint, visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrDim)
		return
	}
}

// renderAbout draws the logo and info panel, stacking them on narrow windows
func (r *OverlayRenderer) renderAbout(body tui.Region) {
	if len(r.cards) == 0 {
		return
	}
	card := &r.cards[0]

	bg := visual.RgbOverlayBg
	fg := visual.RgbOverlayValue
	dimFg := visual.RgbOverlayHint
	headerFg := visual.RgbOverlayHeader

	if body.W < 30 || body.H < 10 {
		body.TextCenter(0, card.Title, headerFg, bg, terminal.AttrBold)
		if len(card.Entries) > 0 {
			body.TextBlock(0, 2, card.Entries[0].Value, fg, bg, terminal.AttrNone)
		}
		return
	}

	if body.W < 50 {
		logoX := (body.W - logoPatternW) / 2
		r.renderLogo(body.Sub(logoX, 0, logoPatternW, logoPatternH), bg)
		info := body.Sub(0, logoPatternH+1, body.W, body.H-logoPatternH-1)
		r.renderAboutInfo(info, bg, fg, dimFg, headerFg, card)
		return
	}

	logoY := (body.H - logoPatternH) / 2
	r.renderLogo(body.Sub(0, logoY, logoPatternW, logoPatternH), bg)
	info := body.Sub(logoPatternW+3, 0, body.W-logoPatternW-3, body.H)
	r.renderAboutInfo(info, bg, fg, dimFg, headerFg, card)
}

func (r *OverlayRenderer) renderAboutInfo(region tui.Region, bg, fg, dimFg, headerFg color.RGB, card *core.OverlayCard) {
	y := 0

	region.Text(0, y, card.Title, headerFg, bg, terminal.AttrBold)
	y += 2

	if len(card.Entries) == 0 {
		return
	}

	// First entry is the description, wrapped
	if y < region.H-4 {
		y += region.TextBlock(0, y, card.Entries[0].Value, fg, bg, terminal.AttrNone) + 1
	}

	keyStyle := tui.Style{Fg: dimFg, Bg: bg}
	valStyle := tui.Style{Fg: fg, Bg: bg}

	for i := 1; i < len(card.Entries); i++ {
		if y >= region.H {
			break
		}
		e := card.Entries[i]
		region.KeyValue(y, e.Key, e.Value, keyStyle, valStyle, ':')
		y++
	}
}

var logoPattern = []string{
	"BBBBBBBBBBBBBBBBBBBBBBBBBB",
	"BByyBBggggggBBbbbbbbBBvvBB",
	"BByyyBBgggggBBbbbbbBBvvvBB",
	"BByyyyBBggggBBbbbbBBvvvvBB",
	"BByyyyyBBgggBBbbbBBvvvvvBB",
	"BByyyyyyBBggBBbbBBvvvvvvBB",
	"BBBBBBBBBBBBBBBBBBBBBBBBBB",
	"BBooooooBBrrBBaaBBppppppBB",
	"BBoooooBBrrrBBaaaBBpppppBB",
	"BBooooBBrrrrBBaaaaBBppppBB",
	"BBoooBBrrrrrBBaaaaaBBpppBB",
	"BBooBBrrrrrrBBaaaaaaBBppBB",
	"BBBBBBBBBBBBBBBBBBBBBBBBBB",
}

var logoColorMap = map[rune]color.RGB{
	'B': {R: 30, G: 30, B: 40},    // Black (frame)
	'r': {R: 255, G: 60, B: 60},   // Red
	'o': {R: 255, G: 165, B: 60},  // Orange
	'y': {R: 255, G: 255, B: 60},  // Yellow
	'g': {R: 60, G: 180, B: 60},   // Green
	'b': {R: 60, G: 100, B: 255},  // Blue
	'v': {R: 100, G: 60, B: 180},  // Violet
	'p': {R: 220, G: 130, B: 220}, // Pink
	'a': {R: 160, G: 160, B: 170}, // Light gray
}

const (
	logoPatternW = 26
	logoPatternH = 13
)

func (r *OverlayRenderer) renderLogo(region tui.Region, bg color.RGB) {
	w, h := region.W, region.H
	if w < 1 || h < 1 {
		return
	}

	for y := range h {
		// Map region Y to pattern Y
		patY := y * logoPatternH / h
		if patY >= logoPatternH {
			patY = logoPatternH - 1
		}
		row := logoPattern[patY]

		for x := range w {
			// Map region X to pattern X
			patX := x * logoPatternW / w
			if patX >= logoPatternW {
				patX = logoPatternW - 1
			}

			ch := rune(row[patX])
			c := logoColorMap[ch]
			region.Cell(x, y, '█', c, bg, terminal.AttrNone)
		}
	}
}
