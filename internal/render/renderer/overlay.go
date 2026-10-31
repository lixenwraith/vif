package renderer

import (
	"math"
	"slices"
	"strings"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
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
	gameCtx   *engine.GameContext
	adapter   *TUIAdapter
	pinMarker rune

	// Layout cache, rebuilt when the content or the content viewport changes
	content *core.OverlayContent
	cards   []core.OverlayCard
	items   []tui.MasonryItem
	blocks  []tui.DocBlock
	masonry *tui.MasonryState
	doc     *tui.DocState
	layoutW int
	layoutH int
	selKey  string // Last selection scrolled into view, a card's or a menu row's

	// The about window's mark, at the frame's cell aspect, on a console in its entries
	logo    logoCells
	aspect  vmath.Aspect
	console *render.Console
}

// NewOverlayRenderer creates a new overlay renderer
func NewOverlayRenderer(gameCtx *engine.GameContext) *OverlayRenderer {
	r := &OverlayRenderer{gameCtx: gameCtx, pinMarker: parameter.OverlayPinMarker}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.pinMarker = parameter.OverlayPinMarker256
		r.console = render.ConsoleFor(cfg.ConsolePalette)
	}
	return r
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
	content := r.gameCtx.GetOverlayContent()
	if ctx.NativeMenu && content != nil && content.Layout == core.OverlayLayoutMenu {
		return
	}

	if r.adapter == nil {
		r.adapter = NewTUIAdapter(g.W, g.H)
	} else if r.adapter.Width() != g.W || r.adapter.Height() != g.H {
		r.adapter.Resize(g.W, g.H)
	}
	r.adapter.Clear(visual.RgbOverlayBg)
	r.aspect = ctx.CellAspect

	root := r.adapter.Region()

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
	case core.OverlayLayoutMenu:
		if data.Menu != nil && data.Menu.Form != nil {
			r.renderForm(body, g, data.Menu.Form)
			r.renderHint(root, g, parameter.OverlayHintsForm)
		} else {
			r.renderMenu(root, body, g, data.Menu)
			switch {
			case data.Menu != nil && slices.ContainsFunc(data.Menu.Rows, func(row core.OverlayMenuRow) bool { return row.Typing }):
				r.renderHint(root, g, parameter.OverlayHintsMenuText)
			default:
				r.renderFilterHint(root, g, parameter.OverlayHintsMenu, parameter.OverlayHintsMenuFilter, parameter.OverlayHintsMenuFilterEdit)
			}
		}
		return

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
		r.renderFilterHint(root, g, parameter.OverlayHintsCards, parameter.OverlayHintsFilter, parameter.OverlayHintsFilterEdit)
	}
}

// syncLayout rebuilds the cached layout when the content or viewport changes
func (r *OverlayRenderer) syncLayout(g engine.OverlayGeometry, data *core.OverlayContent) {
	if r.content == data && r.layoutW == g.ContentW && r.layoutH == g.ContentH {
		return
	}
	r.content, r.layoutW, r.layoutH = data, g.ContentW, g.ContentH
	r.cards = data.Cards()
	r.selKey = "" // Force the selection back into view after a relayout

	switch data.Layout {
	case core.OverlayLayoutMenu:
		if data.Menu != nil {
			r.gameCtx.SetOverlayContentH(len(data.Menu.Rows))
		}
	case core.OverlayLayoutAbout:
		r.gameCtx.SetOverlayContentH(0)
	case core.OverlayLayoutDoc:
		r.buildDoc(g)
	default:
		r.buildCards(g)
	}
}

func (r *OverlayRenderer) renderForm(body tui.Region, g engine.OverlayGeometry, form *core.OverlayForm) {
	if len(form.Fields) == 0 || body.H < 1 {
		return
	}
	state := tui.NewFormState()
	labelW := 0
	for _, f := range form.Fields {
		field := tui.NewTextFieldState(f.Value)
		field.Cursor = f.Cursor
		state.Fields = append(state.Fields, tui.FormField{Label: f.Label, State: field})
		labelW = max(labelW, tui.RuneLen(f.Label)+2)
	}
	listH := min(len(form.Fields), g.ListH())
	scroll := tui.NewScrollState(len(form.Fields), listH)
	scroll.Select(form.Focus)
	state.Fields = state.Fields[scroll.Offset:min(scroll.Offset+listH, len(state.Fields))]
	state.Focus = form.Focus - scroll.Offset
	body.Sub(0, 0, body.W, listH).Form(state, tui.FormOpts{LabelWidth: min(labelW, max(body.W/3, 1)), Style: tui.FormStyle{
		LabelFg: visual.RgbOverlayTitle, FieldFg: visual.RgbOverlayValue, FieldBg: visual.RgbOverlayBg,
		FocusBg: visual.RgbOverlayScrollTrack, CursorFg: visual.RgbOverlayBg, CursorBg: visual.RgbOverlaySelected, Bg: visual.RgbOverlayBg,
	}})
	detail := form.Help
	if form.Error != "" {
		detail = "Error: " + form.Error + "\n" + detail
	}
	for i, line := range tui.WrapText(detail, body.W) {
		y := listH + 1 + i
		if y >= body.H {
			break
		}
		body.Text(0, y, line, visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrNone)
	}
}

func (r *OverlayRenderer) renderMenu(root, body tui.Region, g engine.OverlayGeometry, menu *core.OverlayMenu) {
	if menu == nil || len(menu.Rows) == 0 {
		return
	}
	selected := slices.IndexFunc(menu.Rows, func(row core.OverlayMenuRow) bool { return row.Key == r.gameCtx.GetOverlaySelection() })
	selected = max(selected, 0)
	listH := g.ListH()
	scroll := tui.NewScrollState(len(menu.Rows), listH)
	scroll.ScrollTo(r.gameCtx.GetOverlayScroll())
	// Scroll only when the selection moved, so the wheel and the scrollbar stay free
	if key := menu.Rows[selected].Key; key != r.selKey {
		r.selKey = key
		scroll.EnsureVisible(selected)
	}
	r.gameCtx.SetOverlayScroll(scroll.Offset)

	items := make([]tui.ListItem, len(menu.Rows))
	boxW := max(body.W/2, 1)
	for i, row := range menu.Rows {
		fg := visual.RgbOverlayValue
		if row.Disabled != "" {
			fg = visual.RgbOverlayHint
		}
		bg := visual.RgbOverlayBg
		if i == selected && !row.Header {
			fg, bg = visual.RgbOverlayBg, visual.RgbOverlaySelected
		}
		var text string
		switch {
		case row.Header:
			// A section's rule runs to the scrollbar's edge, its title set in it
			fg, text = visual.RgbOverlayHeader, string(parameter.OverlayMenuRule)+" "+row.Label+" "
			text += strings.Repeat(string(parameter.OverlayMenuRule), max(body.W-2-tui.RuneLen(text), 0))
		case row.Text:
			labelW := max(1, body.W-boxW-3)
			text = tui.PadRight(tui.Truncate(row.Label, labelW), labelW)
		default:
			value := row.Value
			if row.Disabled != "" {
				value = "[" + value + "]"
			}
			valueW := min(tui.RuneLen(value), max(1, body.W/3))
			labelW := max(1, body.W-valueW-3)
			text = tui.PadRight(tui.Truncate(row.Label, labelW), labelW) + " " + tui.Truncate(value, valueW)
		}
		items[i] = tui.ListItem{Text: text, TextStyle: tui.Style{Fg: fg, Bg: bg}}
	}
	list := body.Sub(0, 0, body.W, listH)
	list.List(items, selected, scroll.Offset, tui.ListOpts{
		CursorBg: visual.RgbOverlaySelected, DefaultBg: visual.RgbOverlayBg, IconWidth: 1,
	})
	for y := range listH {
		if i := scroll.Offset + y; i < len(menu.Rows) && menu.Rows[i].Text {
			r.renderMenuTextBox(list.Sub(body.W-boxW-1, y, boxW, 1), menu.Rows[i])
		}
	}
	if g.ScrollW > 0 {
		root.Sub(g.ScrollX, g.ContentY, g.ScrollW, listH).ScrollBarStyled(0, scroll.Offset, listH, len(menu.Rows), tui.ScrollBarOpts{
			ThumbFg: visual.RgbOverlayScrollThumb, TrackFg: visual.RgbOverlayScrollTrack, Bg: visual.RgbOverlayBg,
		})
	}
	row := menu.Rows[selected]
	detail := row.Description
	if row.Disabled != "" && row.Disabled != "Read only" {
		detail = row.Disabled + ". " + detail
	}
	for i, line := range tui.WrapText(detail, body.W) {
		y := listH + 1 + i
		if y >= body.H {
			break
		}
		body.Text(0, y, line, visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrNone)
	}
}

// renderMenuTextBox draws a text row's box, its cursor shown while it is typed in
func (r *OverlayRenderer) renderMenuTextBox(box tui.Region, row core.OverlayMenuRow) {
	state := tui.NewTextFieldState(row.Value)
	state.Cursor = row.Cursor
	placeholder := parameter.OverlayTextPlaceholder
	if row.Disabled != "" {
		placeholder = parameter.OverlayTextUnavailable
	}
	box.TextField(state, tui.TextFieldOpts{Placeholder: placeholder, Focused: row.Typing, Border: tui.LineNone, Style: tui.TextFieldStyle{
		TextFg: visual.RgbOverlayValue, TextBg: visual.RgbOverlayScrollTrack, CursorFg: visual.RgbOverlayBg,
		CursorBg: visual.RgbOverlaySelected, PlaceholderFg: visual.RgbOverlayHint,
	}})
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
	if len(r.cards) == 0 {
		if q := r.gameCtx.OverlayFilter(); q != "" {
			body.TextCenter(body.H/2, "no group or metric matches /"+q,
				visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrDim)
		}
		return
	}
	if r.masonry == nil {
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
			title = " " + string(r.pinMarker) + " " + card.Title + " "
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

// renderFilterHint puts the query's prompt left of the hints while it is edited
// (editing) or kept (kept), otherwise draws the plain hints
func (r *OverlayRenderer) renderFilterHint(root tui.Region, g engine.OverlayGeometry, plain, kept, editing []string) {
	query, typing := r.gameCtx.OverlayFilter(), r.gameCtx.IsOverlayFilterEditing()
	if query == "" && !typing {
		r.renderHint(root, g, plain)
		return
	}
	if g.HintY < 0 {
		return
	}

	avail := g.W - 4 // one cell of margin inside each border
	prompt, tiers := "/"+query, kept
	if typing {
		prompt, tiers = prompt+string(parameter.StatusCursorChar), editing
	}
	prompt = tui.TruncateLeft(prompt, avail)
	root.Text(2, g.HintY, prompt, visual.RgbSearchInputText, visual.RgbOverlayBg, terminal.AttrNone)

	free := avail - tui.RuneLen(prompt) - 2 // gap between prompt and hints
	for _, hint := range tiers {
		if n := tui.RuneLen(hint); n <= free {
			root.Text(2+avail-n, g.HintY, hint, visual.RgbOverlayHint, visual.RgbOverlayBg, terminal.AttrDim)
			return
		}
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

	if body.W < parameter.AboutMinWidth || body.H < parameter.AboutMinHeight {
		body.TextCenter(0, card.Title, headerFg, bg, terminal.AttrBold)
		if len(card.Entries) > 0 {
			body.TextBlock(0, 2, card.Entries[0].Value, fg, bg, terminal.AttrNone)
		}
		return
	}

	stacked := body.W < parameter.AboutStackWidth
	h := min(parameter.AboutLogoRows, body.H)
	if stacked {
		h = min(h, body.H/2)
	}
	w := int(math.Round(float64(h) / float64(r.aspect)))
	if stacked {
		r.renderLogo(body.Sub((body.W-w)/2, 0, w, h), bg)
		info := body.Sub(0, h+1, body.W, body.H-h-1)
		r.renderAboutInfo(info, bg, fg, dimFg, headerFg, card)
		return
	}

	// Side by side, the two centred as one block
	info := body.Sub(w+parameter.AboutLogoGap, 0, body.W-w-parameter.AboutLogoGap, body.H)
	top := max(body.H-max(h, aboutInfoRows(info.W, card)), 0) / 2
	r.renderLogo(body.Sub(0, top, w, h), bg)
	r.renderAboutInfo(info.Sub(0, top, info.W, info.H-top), bg, fg, dimFg, headerFg, card)
}

// aboutInfoRows is the rows renderAboutInfo takes w columns wide
func aboutInfoRows(w int, card *core.OverlayCard) int {
	if len(card.Entries) == 0 {
		return 1
	}
	return 2 + len(tui.WrapText(card.Entries[0].Value, w)) + len(card.Entries)
}

// renderLogo draws the mark opaque: a quadrant's black is the stroke's, not see-through
func (r *OverlayRenderer) renderLogo(region tui.Region, bg color.RGB) {
	for i, c := range r.logo.layout(region.W, region.H, r.aspect, bg, r.console) {
		if c.Rune != 0 {
			region.CellOpaque(i%region.W, i/region.W, c.Rune, c.Fg, c.Bg, terminal.AttrNone)
		}
	}
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

	// The keys right-aligned on one column, so the values start together
	keyW := 0
	for _, e := range card.Entries[1:] {
		keyW = max(keyW, tui.RuneLen(e.Key))
	}
	for i := 1; i < len(card.Entries); i++ {
		if y >= region.H {
			break
		}
		e := card.Entries[i]
		region.KeyValueColumn(y, keyW, e.Key, e.Value, keyStyle, valStyle, ' ')
		y++
	}
}
