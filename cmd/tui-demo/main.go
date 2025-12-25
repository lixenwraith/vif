package main

import (
	"os"
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// Colors
var (
	bgColor     = terminal.RGB{R: 20, G: 20, B: 30}
	fgColor     = terminal.RGB{R: 200, G: 200, B: 200}
	borderColor = terminal.RGB{R: 80, G: 100, B: 140}
	accentColor = terminal.RGB{R: 100, G: 200, B: 220}
	warnColor   = terminal.RGB{R: 255, G: 180, B: 100}
	goodColor   = terminal.RGB{R: 80, G: 200, B: 80}
	dimColor    = terminal.RGB{R: 100, G: 100, B: 100}
	headerBg    = terminal.RGB{R: 40, G: 50, B: 70}
)

func main() {
	term := terminal.New()
	if err := term.Init(); err != nil {
		os.Exit(1)
	}
	defer term.Fini()

	// Dedicated input goroutine
	eventCh := make(chan terminal.Event, 16)
	go func() {
		for {
			ev := term.PollEvent()
			eventCh <- ev
			if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
				return
			}
		}
	}()

	frame := 0
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()

	// Simulated scroll state
	listScroll := 0
	listItems := 25
	listCursor := 3

	for {
		// Handle all pending events
	eventLoop:
		for {
			select {
			case ev := <-eventCh:
				if ev.Type == terminal.EventKey {
					if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyEscape ||
						(ev.Key == terminal.KeyRune && ev.Rune == 'q') {
						return
					}
					if ev.Key == terminal.KeyDown || (ev.Key == terminal.KeyRune && ev.Rune == 'j') {
						listCursor++
					}
					if ev.Key == terminal.KeyUp || (ev.Key == terminal.KeyRune && ev.Rune == 'k') {
						listCursor--
					}
				}
				if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
					return
				}
			default:
				break eventLoop
			}
		}

		// Wait for next frame
		select {
		case <-ticker.C:
			frame++
		case ev := <-eventCh:
			// Handle event that arrived while waiting
			if ev.Type == terminal.EventKey {
				if ev.Key == terminal.KeyCtrlC || ev.Key == terminal.KeyEscape ||
					(ev.Key == terminal.KeyRune && ev.Rune == 'q') {
					return
				}
				if ev.Key == terminal.KeyDown || (ev.Key == terminal.KeyRune && ev.Rune == 'j') {
					listCursor++
				}
				if ev.Key == terminal.KeyUp || (ev.Key == terminal.KeyRune && ev.Rune == 'k') {
					listCursor--
				}
			}
			if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
				return
			}
		}

		w, h := term.Size()
		cells := make([]terminal.Cell, w*h)

		root := tui.NewRegion(cells, w, 0, 0, w, h)
		root.Fill(bgColor)

		// Clamp cursor and adjust scroll
		listCursor = tui.ClampCursor(listCursor, listItems)
		visibleItems := 8
		listScroll = tui.AdjustScroll(listCursor, listScroll, visibleItems, listItems)

		render(root, frame, listScroll, listCursor, listItems, visibleItems)

		term.Flush(cells, w, h)
	}
}

func render(root tui.Region, frame, listScroll, listCursor, listItems, visibleItems int) {
	// Header bar
	header, body := tui.SplitVFixed(root, 1)
	header.Fill(headerBg)
	header.Text(1, 0, "TUI DEMO", accentColor, headerBg, terminal.AttrBold)
	header.TextCenter(0, "terminal/tui showcase", fgColor, headerBg, terminal.AttrNone)

	sizeInfo := padInt(root.W) + "x" + padInt(root.H)
	header.TextRight(0, sizeInfo+" ", dimColor, headerBg, terminal.AttrNone)

	// Footer
	content, footer := tui.SplitVFixed(body, body.H-1)
	footer.Fill(headerBg)
	footer.Text(1, 0, "q/Esc: quit | j/k: scroll list | Responsive: resize terminal", dimColor, headerBg, terminal.AttrNone)

	// Responsive layout based on width
	switch tui.BreakpointH(content.W, 100, 60) {
	case 0: // Wide: 3 columns
		cols := tui.SplitH(content, 0.33, 0.34, 0.33)
		renderBoxStyles(cols[0].Inset(1))
		renderProgressSection(cols[1].Inset(1), frame)
		renderTextSection(cols[2].Inset(1), listScroll, listCursor, listItems, visibleItems)
	case 1: // Medium: 2 columns
		cols := tui.SplitH(content, 0.5, 0.5)
		parts := tui.SplitV(cols[0], 0.5, 0.5)
		renderBoxStyles(parts[0].Inset(1))
		renderProgressSection(parts[1].Inset(1), frame)
		renderTextSection(cols[1].Inset(1), listScroll, listCursor, listItems, visibleItems)
	default: // Narrow: stacked
		sections := tui.SplitV(content, 0.35, 0.35, 0.30)
		renderBoxStyles(sections[0].Inset(1))
		renderProgressSection(sections[1].Inset(1), frame)
		renderTextSection(sections[2].Inset(1), listScroll, listCursor, listItems, visibleItems)
	}
}

func renderBoxStyles(r tui.Region) {
	inner := r.Card("BOX STYLES", tui.LineDouble, borderColor)

	// Grid of box style examples
	if inner.W >= 40 && inner.H >= 10 {
		grid := tui.GridLayout(inner, 2, 2, 1, 1)

		// Single line box
		grid[0].Box(tui.LineSingle, borderColor)
		grid[0].TextCenter(0, " Single ", dimColor, bgColor, terminal.AttrNone)
		grid[0].Inset(1).Text(0, 0, "LineSingle", fgColor, bgColor, terminal.AttrNone)

		// Double line box
		grid[1].Box(tui.LineDouble, accentColor)
		grid[1].TextCenter(0, " Double ", dimColor, bgColor, terminal.AttrNone)
		grid[1].Inset(1).Text(0, 0, "LineDouble", fgColor, bgColor, terminal.AttrNone)

		// Rounded box
		grid[2].Box(tui.LineRounded, goodColor)
		grid[2].TextCenter(0, " Rounded ", dimColor, bgColor, terminal.AttrNone)
		grid[2].Inset(1).Text(0, 0, "LineRounded", fgColor, bgColor, terminal.AttrNone)

		// Heavy box
		grid[3].Box(tui.LineHeavy, warnColor)
		grid[3].TextCenter(0, " Heavy ", dimColor, bgColor, terminal.AttrNone)
		grid[3].Inset(1).Text(0, 0, "LineHeavy", fgColor, bgColor, terminal.AttrNone)
	} else {
		// Compact: just list them
		inner.Text(0, 0, "Single ─", borderColor, bgColor, terminal.AttrNone)
		inner.Text(0, 1, "Double ═", accentColor, bgColor, terminal.AttrNone)
		inner.Text(0, 2, "Rounded ─", goodColor, bgColor, terminal.AttrNone)
		inner.Text(0, 3, "Heavy ━", warnColor, bgColor, terminal.AttrNone)
	}
}

func renderProgressSection(r tui.Region, frame int) {
	inner := r.Card("PROGRESS & INDICATORS", tui.LineDouble, borderColor)

	y := 0

	// Spinner
	inner.Text(0, y, "Spinner:", dimColor, bgColor, terminal.AttrNone)
	inner.Spinner(10, y, frame, accentColor)
	y++

	// Checkboxes
	y++
	inner.Text(0, y, "Checkboxes:", dimColor, bgColor, terminal.AttrNone)
	y++
	inner.Checkbox(0, y, tui.CheckNone, dimColor)
	inner.Text(4, y, "None", fgColor, bgColor, terminal.AttrNone)
	inner.Checkbox(12, y, tui.CheckPartial, warnColor)
	inner.Text(16, y, "Partial", fgColor, bgColor, terminal.AttrNone)
	y++
	inner.Checkbox(0, y, tui.CheckFull, goodColor)
	inner.Text(4, y, "Full", fgColor, bgColor, terminal.AttrNone)
	inner.Checkbox(12, y, tui.CheckPlus, accentColor)
	inner.Text(16, y, "Plus", fgColor, bgColor, terminal.AttrNone)
	y += 2

	// Horizontal progress bars
	inner.Text(0, y, "Progress bars:", dimColor, bgColor, terminal.AttrNone)
	y++

	barW := inner.W - 8
	if barW < 10 {
		barW = 10
	}

	inner.Text(0, y, "25%", dimColor, bgColor, terminal.AttrNone)
	inner.Progress(5, y, barW, 0.25, accentColor, dimColor)
	y++

	inner.Text(0, y, "60%", dimColor, bgColor, terminal.AttrNone)
	inner.Progress(5, y, barW, 0.60, warnColor, dimColor)
	y++

	inner.Text(0, y, "90%", dimColor, bgColor, terminal.AttrNone)
	inner.Progress(5, y, barW, 0.90, goodColor, dimColor)
	y += 2

	// Gauges
	inner.Text(0, y, "Gauges:", dimColor, bgColor, terminal.AttrNone)
	y++

	gaugeW := inner.W - 2
	if gaugeW < 15 {
		gaugeW = 15
	}

	inner.Gauge(0, y, gaugeW, 45, 100, accentColor, dimColor)
	y++
	inner.Gauge(0, y, gaugeW, 100, 100, goodColor, dimColor)
	y += 2

	// Vertical progress (if space)
	if inner.H-y >= 5 && inner.W >= 20 {
		inner.Text(0, y, "Vertical:", dimColor, bgColor, terminal.AttrNone)
		vH := inner.H - y - 1
		if vH > 6 {
			vH = 6
		}
		inner.ProgressV(10, y, vH, 0.33, accentColor, dimColor)
		inner.ProgressV(12, y, vH, 0.66, warnColor, dimColor)
		inner.ProgressV(14, y, vH, 1.0, goodColor, dimColor)
	}
}

func renderTextSection(r tui.Region, listScroll, listCursor, listItems, visibleItems int) {
	parts := tui.SplitV(r, 0.45, 0.55)

	// Text examples card
	textCard := parts[0].Card("TEXT UTILITIES", tui.LineDouble, borderColor)
	renderTextExamples(textCard)

	// Scrollable list card
	listCard := parts[1].Card("SCROLLABLE LIST (j/k)", tui.LineDouble, borderColor)
	renderScrollableList(listCard, listScroll, listCursor, listItems, visibleItems)
}

func renderTextExamples(r tui.Region) {
	y := 0

	// Alignment
	r.Text(0, y, "Alignment:", dimColor, bgColor, terminal.AttrNone)
	y++
	r.Text(0, y, "Left", fgColor, bgColor, terminal.AttrNone)
	r.TextCenter(y, "Center", accentColor, bgColor, terminal.AttrNone)
	r.TextRight(y, "Right", warnColor, bgColor, terminal.AttrNone)
	y += 2

	// Truncation
	r.Text(0, y, "Truncation:", dimColor, bgColor, terminal.AttrNone)
	y++

	longText := "This is a very long string that needs truncation"
	truncW := r.W - 2
	if truncW < 10 {
		truncW = 10
	}

	r.Text(0, y, tui.Truncate(longText, truncW), fgColor, bgColor, terminal.AttrNone)
	y++
	r.Text(0, y, tui.TruncateLeft(longText, truncW), accentColor, bgColor, terminal.AttrNone)
	y++
	r.Text(0, y, tui.TruncateMiddle(longText, truncW), warnColor, bgColor, terminal.AttrNone)
	y += 2

	// Padding
	if y < r.H-2 {
		r.Text(0, y, "Padding:", dimColor, bgColor, terminal.AttrNone)
		y++
		r.Text(0, y, "|"+tui.PadRight("Right", 12)+"|", fgColor, bgColor, terminal.AttrNone)
		y++
		if y < r.H {
			r.Text(0, y, "|"+tui.PadCenter("Center", 12)+"|", fgColor, bgColor, terminal.AttrNone)
		}
	}
}

func renderScrollableList(r tui.Region, scroll, cursor, total, visible int) {
	// Reserve rightmost column for scrollbar
	listArea, scrollCol := tui.SplitHFixed(r, r.W-1)

	// Draw list items
	for i := 0; i < visible && scroll+i < total; i++ {
		itemIdx := scroll + i
		y := i

		isCursor := itemIdx == cursor
		fg := fgColor
		bg := bgColor

		if isCursor {
			bg = terminal.RGB{R: 50, G: 50, B: 70}
			fg = accentColor
		}

		// Fill row background
		for x := 0; x < listArea.W; x++ {
			listArea.Cell(x, y, ' ', fg, bg, terminal.AttrNone)
		}

		// Checkbox
		state := tui.CheckNone
		if itemIdx%3 == 0 {
			state = tui.CheckFull
		} else if itemIdx%5 == 0 {
			state = tui.CheckPartial
		}
		listArea.Checkbox(0, y, state, goodColor)

		// Item text
		itemText := "Item " + padInt(itemIdx+1) + " - " + itemDescription(itemIdx)
		itemText = tui.Truncate(itemText, listArea.W-5)
		listArea.Text(4, y, itemText, fg, bg, terminal.AttrNone)
	}

	// Scrollbar
	tui.ScrollBar(scrollCol, 0, scroll, visible, total, dimColor)

	// Scroll indicator at bottom
	if r.H > visible {
		tui.ScrollIndicator(r, r.H-1, scroll, visible, total, dimColor)
	}
}

func itemDescription(idx int) string {
	descriptions := []string{
		"Configuration file",
		"User preferences",
		"System settings",
		"Network config",
		"Display options",
		"Audio settings",
		"Input mapping",
		"Debug logging",
	}
	return descriptions[idx%len(descriptions)]
}

func padInt(n int) string {
	if n < 10 {
		return " " + string(rune('0'+n))
	}
	if n < 100 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0'+n/100)) + string(rune('0'+(n/10)%10)) + string(rune('0'+n%10))
}