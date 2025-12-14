package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Help content for pane view only (mindmap/dive removed)
var helpPaneKeys = []helpEntry{
	{"Navigation", ""},
	{"j/k, ↑/↓", "Move cursor"},
	{"h/l, ←/→", "Collapse/expand"},
	{"H/L", "Collapse/expand all"},
	{"0/$, Home/End", "Jump start/end"},
	{"PgUp/PgDn", "Page scroll"},
	{"Tab/S-Tab", "Next/prev pane"},
	{"", ""},
	{"Selection", ""},
	{"Space", "Toggle selection"},
	{"a", "Select all visible"},
	{"c", "Clear selections"},
	{"F", "Select filtered"},
	{"", ""},
	{"Filter", ""},
	{"f", "Filter at cursor"},
	{"/", "Content search"},
	{"m", "Cycle filter mode"},
	{"Esc", "Clear filter"},
	{"", ""},
	{"Other", ""},
	{"d", "Toggle deps"},
	{"+/-", "Deps depth"},
	{"e", "Edit tags"},
	{"r", "Reindex"},
	{"p", "Preview output"},
	{"Ctrl+S", "Save output"},
	{"Ctrl+Y", "Copy to clipboard"},
	{"Ctrl+Q", "Quit"},
}

var helpEditorKeys = []helpEntry{
	{"Editor Keys", ""},
	{"←/→", "Move cursor"},
	{"Home/Ctrl+A", "Go to start"},
	{"End/Ctrl+E", "Go to end"},
	{"Backspace", "Delete before"},
	{"Delete", "Delete at cursor"},
	{"Ctrl+K", "Kill to end"},
	{"Ctrl+U", "Kill to start"},
	{"Ctrl+W", "Kill word"},
	{"Enter", "Save changes"},
	{"Escape", "Cancel edit"},
	{"", ""},
	{"Tag Syntax", ""},
	{"2-level", "group(tag1,tag2)"},
	{"3-level", "group[mod(tag)]"},
	{"", ""},
	{"Examples", ""},
	{"#focus{}", "Classification"},
	{"#interact{}", "Relationships"},
	{"all(*)", "Always include"},
}

type helpEntry struct {
	Key  string
	Desc string
}

// RenderHelp draws the help overlay
func (app *AppState) RenderHelp(cells []terminal.Cell, w, h int) {
	// Background
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	// Border
	drawDoubleFrame(cells, w, 0, 0, w, h)

	// Title
	title := " HELP "
	drawText(cells, w, (w-len(title))/2, 0, title, colorHeaderFg, colorDefaultBg, terminal.AttrBold)

	// Close hint
	hint := "[?/Esc: close]"
	drawText(cells, w, w-len(hint)-2, 0, hint, colorHelpFg, colorDefaultBg, terminal.AttrNone)

	// Calculate column layout - two columns
	colWidth := (w - 6) / 2
	col1X := 3
	col2X := col1X + colWidth + 2

	// Column headers
	headerY := 2
	drawText(cells, w, col1X, headerY, "PANE VIEW", colorGroupFg, colorDefaultBg, terminal.AttrBold)
	drawText(cells, w, col2X, headerY, "TAG EDITOR", colorGroupFg, colorDefaultBg, terminal.AttrBold)

	// Separator line
	sepY := headerY + 1
	for x := 1; x < w-1; x++ {
		cells[sepY*w+x] = terminal.Cell{Rune: '─', Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	// Content start
	contentY := sepY + 1
	maxRows := h - contentY - 2

	// Render columns
	renderHelpColumn(cells, w, col1X, contentY, colWidth, maxRows, helpPaneKeys)
	renderHelpColumn(cells, w, col2X, contentY, colWidth, maxRows, helpEditorKeys)

	// Vertical separator between columns
	for y := headerY; y < h-1; y++ {
		cells[y*w+col2X-1] = terminal.Cell{Rune: '│', Fg: colorPaneBorder, Bg: colorDefaultBg}
	}
}

// renderHelpColumn draws a single column of help entries
func renderHelpColumn(cells []terminal.Cell, totalW, x, y, colW, maxRows int, entries []helpEntry) {
	row := 0
	for _, e := range entries {
		if row >= maxRows {
			break
		}

		if e.Key == "" && e.Desc == "" {
			// Empty line spacer
			row++
			continue
		}

		if e.Desc == "" {
			// Section header
			drawText(cells, totalW, x, y+row, e.Key, colorExpandedFg, colorDefaultBg, terminal.AttrBold)
			row++
			continue
		}

		// Key-description pair
		keyStr := e.Key
		keyRuneLen := len([]rune(keyStr))
		maxKeyLen := 14
		if keyRuneLen > maxKeyLen {
			runes := []rune(keyStr)
			keyStr = string(runes[:maxKeyLen-1]) + "…"
		}
		drawText(cells, totalW, x, y+row, keyStr, colorTagFg, colorDefaultBg, terminal.AttrNone)

		descX := x + maxKeyLen + 1
		descMaxLen := colW - maxKeyLen - 2
		desc := e.Desc
		descRuneLen := len([]rune(desc))
		if descRuneLen > descMaxLen && descMaxLen > 3 {
			runes := []rune(desc)
			desc = string(runes[:descMaxLen-1]) + "…"
		}
		drawText(cells, totalW, descX, y+row, desc, colorDefaultFg, colorDefaultBg, terminal.AttrNone)
		row++
	}
}

// HandleHelpEvent processes input in help overlay
func (app *AppState) HandleHelpEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.HelpMode = false
	case terminal.KeyRune:
		if ev.Rune == '?' || ev.Rune == 'q' {
			app.HelpMode = false
		}
	}
}