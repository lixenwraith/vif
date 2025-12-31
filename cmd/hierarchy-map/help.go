package main

import (
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// HelpState manages help overlay state
type HelpState struct {
	Visible bool
	Scroll  int
}

// HelpEntry represents a single key binding
type HelpEntry struct {
	Key  string
	Desc string
}

// HelpColumn represents a column in the help overlay
type HelpColumn struct {
	Title   string
	Entries []HelpEntry
}

var helpMain = HelpColumn{
	Title: "MAIN VIEW",
	Entries: []HelpEntry{
		{"Ctrl+C/Q", "Quit"},
		{"?", "Toggle help"},
		{"Tab", "Next pane"},
		{"S-Tab", "Previous pane"},
		{"", ""},
		{"e", "Open editor"},
		{"/", "Search mode"},
		{"f", "Filter current pane"},
		{"F", "Select filtered files"},
		{"m", "Cycle filter mode"},
		{"Esc", "Clear filter"},
		{"", ""},
		{"r", "Reindex all"},
		{"d", "Toggle dep expansion"},
		{"+/-", "Adjust depth limit"},
		{"c", "Clear selections"},
		{"", ""},
		{"Ctrl+S", "Save output"},
		{"Ctrl+L", "Load selection"},
		{"", ""},
		{"─── PANE NAV ───", ""},
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"h/←", "Collapse"},
		{"l/→", "Expand"},
		{"Space", "Toggle selection"},
		{"s", "Select & advance"},
		{"a", "Select all visible"},
		{"PgUp/Dn", "Page scroll"},
		{"0/$", "Jump start/end"},
		{"H/L", "Collapse/Expand all"},
		{"Enter", "Open file (Tree)"},
	},
}

var helpViewer = HelpColumn{
	Title: "FILE VIEWER",
	Entries: []HelpEntry{
		{"Esc/q", "Close viewer"},
		{"", ""},
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"g", "Jump to start"},
		{"G", "Jump to end"},
		{"PgUp/Dn", "Page scroll"},
		{"Ctrl+U/D", "Half page scroll"},
		{"", ""},
		{"─── SEARCH ───", ""},
		{"/", "Enter search"},
		{"n", "Next match"},
		{"N", "Previous match"},
		{"", ""},
		{"─── FOLDING ───", ""},
		{"o", "Toggle fold"},
		{"h/←", "Collapse fold"},
		{"l/→/Enter", "Expand fold"},
		{"M", "Collapse all folds"},
		{"R", "Expand all folds"},
	},
}

var helpEditor = HelpColumn{
	Title: "TAG EDITOR",
	Entries: []HelpEntry{
		{"Esc", "Close editor"},
		{"Ctrl+S", "Save changes"},
		{"Tab", "Next pane"},
		{"S-Tab", "Previous pane"},
		{"", ""},
		{"─── TAGS PANE ───", ""},
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"h/←", "Collapse"},
		{"l/→", "Expand"},
		{"g/G", "Jump start/end"},
		{"Space/d", "Toggle deletion"},
		{"PgUp/Dn", "Page scroll"},
		{"", ""},
		{"─── RAW PANE ───", ""},
		{"Enter", "Add tag"},
		{"(text input)", ""},
		{"", ""},
		{"─── FILES PANE ───", ""},
		{"j/↓", "Move down"},
		{"k/↑", "Move up"},
		{"g/G", "Jump start/end"},
		{"PgUp/Dn", "Page scroll"},
	},
}

// renderHelp draws the help overlay
func (app *AppState) renderHelp(r tui.Region) {
	if app.Help == nil || !app.Help.Visible {
		return
	}

	content := r.Modal(tui.ModalOpts{
		Title:    "KEYBOARD SHORTCUTS",
		Hint:     "Esc/q/?:close  j/k:scroll",
		Border:   tui.LineDouble,
		BorderFg: app.Theme.Border,
		TitleFg:  app.Theme.HeaderFg,
		HintFg:   app.Theme.StatusFg,
		Bg:       app.Theme.Bg,
	})

	columns := []HelpColumn{helpMain, helpViewer, helpEditor}
	colW := content.W / 3

	// Determine max rows needed
	maxRows := 0
	for _, col := range columns {
		if len(col.Entries) > maxRows {
			maxRows = len(col.Entries)
		}
	}

	// Render columns
	for ci, col := range columns {
		colX := ci * colW
		colRegion := content.Sub(colX, 0, colW, content.H)

		// Column title
		colRegion.TextCenter(0, col.Title, app.Theme.HeaderFg, app.Theme.Bg, terminal.AttrBold)

		// Separator line
		for x := 1; x < colRegion.W-1; x++ {
			colRegion.Cell(x, 1, '─', app.Theme.Border, app.Theme.Bg, terminal.AttrDim)
		}

		// Entries
		keyW := 12
		for i, entry := range col.Entries {
			y := i + 2 - app.Help.Scroll
			if y < 2 || y >= colRegion.H {
				continue
			}

			// Section header (starts with ─)
			if len(entry.Key) > 0 && entry.Key[0] == '\xe2' { // UTF-8 for ─
				colRegion.Text(1, y, entry.Key, app.Theme.Border, app.Theme.Bg, terminal.AttrDim)
				continue
			}

			// Empty line
			if entry.Key == "" && entry.Desc == "" {
				continue
			}

			// Key
			keyFg := app.Theme.TagFg
			colRegion.Text(1, y, entry.Key, keyFg, app.Theme.Bg, terminal.AttrBold)

			// Description
			colRegion.Text(keyW+1, y, entry.Desc, app.Theme.Fg, app.Theme.Bg, terminal.AttrNone)
		}

		// Vertical separator between columns (except last)
		if ci < len(columns)-1 {
			sepX := colW - 1
			for y := 0; y < colRegion.H; y++ {
				colRegion.Cell(sepX, y, '│', app.Theme.Border, app.Theme.Bg, terminal.AttrDim)
			}
		}
	}
}

// handleHelpEvent processes keyboard input for help overlay
func (app *AppState) handleHelpEvent(ev terminal.Event) bool {
	if app.Help == nil || !app.Help.Visible {
		return false
	}

	maxScroll := len(helpMain.Entries) - 10
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.Help.Visible = false
		return true

	case terminal.KeyUp:
		if app.Help.Scroll > 0 {
			app.Help.Scroll--
		}
		return true

	case terminal.KeyDown:
		if app.Help.Scroll < maxScroll {
			app.Help.Scroll++
		}
		return true

	case terminal.KeyPageUp:
		app.Help.Scroll -= 10
		if app.Help.Scroll < 0 {
			app.Help.Scroll = 0
		}
		return true

	case terminal.KeyPageDown:
		app.Help.Scroll += 10
		if app.Help.Scroll > maxScroll {
			app.Help.Scroll = maxScroll
		}
		return true

	case terminal.KeyRune:
		switch ev.Rune {
		case 'q', '?':
			app.Help.Visible = false
			return true
		case 'j':
			if app.Help.Scroll < maxScroll {
				app.Help.Scroll++
			}
			return true
		case 'k':
			if app.Help.Scroll > 0 {
				app.Help.Scroll--
			}
			return true
		}
	}

	return true
}

// ToggleHelp shows or hides the help overlay
func (app *AppState) ToggleHelp() {
	if app.Help == nil {
		app.Help = &HelpState{}
	}
	app.Help.Visible = !app.Help.Visible
	app.Help.Scroll = 0
}