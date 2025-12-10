package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// HandleEvent processes a key event
func (app *AppState) HandleEvent(ev terminal.Event) (quit, output bool) {
	app.Message = ""

	if app.PreviewMode {
		return app.handlePreviewEvent(ev)
	}

	if app.InputMode {
		return app.handleInputEvent(ev)
	}

	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			return true, false
		case 'j':
			app.MoveCursor(1)
		case 'k':
			app.MoveCursor(-1)
		case ' ':
			app.ToggleSelection()
		case '/':
			if app.RgAvailable {
				app.InputMode = true
				app.InputBuffer = ""
			} else {
				app.Message = "ripgrep (rg) not found"
			}
		case 'g':
			app.CycleGroup()
		case 'd':
			app.ExpandDeps = !app.ExpandDeps
			if app.ExpandDeps {
				app.Message = "dependency expansion ON"
			} else {
				app.Message = "dependency expansion OFF"
			}
		case '+', '=':
			if app.DepthLimit < 5 {
				app.DepthLimit++
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
		case '-':
			if app.DepthLimit > 1 {
				app.DepthLimit--
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
		case 'a':
			for _, name := range app.PackageList {
				app.Selected[name] = true
			}
			app.Message = "selected all visible"
		case 'c':
			app.Selected = make(map[string]bool)
			app.Message = "cleared selection"
		case 'i':
			app.CaseSensitive = !app.CaseSensitive
			if app.CaseSensitive {
				app.Message = "case sensitive ON"
			} else {
				app.Message = "case sensitive OFF"
			}
		case 'p':
			app.EnterPreview()
		}

	case terminal.KeyUp:
		app.MoveCursor(-1)
	case terminal.KeyDown:
		app.MoveCursor(1)
	case terminal.KeyEnter:
		return false, true
	case terminal.KeyEscape:
		if app.KeywordFilter != "" {
			app.KeywordFilter = ""
			app.KeywordMatches = make(map[string]bool)
			app.UpdatePackageList()
			app.Message = "keyword filter cleared"
		}
	}

	return false, false
}

// handleInputEvent handles keyboard input in input mode
func (app *AppState) handleInputEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputBuffer = ""
		return false, false

	case terminal.KeyEnter:
		app.InputMode = false
		if app.InputBuffer != "" {
			app.KeywordFilter = app.InputBuffer
			matches, err := SearchKeyword(".", app.KeywordFilter, app.CaseSensitive)
			if err != nil {
				app.Message = "search error: " + err.Error()
				app.KeywordFilter = ""
			} else {
				app.KeywordMatches = make(map[string]bool)
				for _, m := range matches {
					app.KeywordMatches[m] = true
				}
				app.Message = fmt.Sprintf("found %d files", len(matches))
			}
			app.UpdatePackageList()
		}
		app.InputBuffer = ""
		return false, false

	case terminal.KeyBackspace:
		if len(app.InputBuffer) > 0 {
			app.InputBuffer = app.InputBuffer[:len(app.InputBuffer)-1]
		}
		return false, false

	case terminal.KeyRune:
		app.InputBuffer += string(ev.Rune)
		return false, false
	}

	return false, false
}

// handlePreviewEvent handles keyboard input in preview mode
func (app *AppState) handlePreviewEvent(ev terminal.Event) (quit, output bool) {
	maxScroll := len(app.PreviewFiles) - (app.Height - headerHeight - 2)
	if maxScroll < 0 {
		maxScroll = 0
	}

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyRune:
		if ev.Key == terminal.KeyEscape || ev.Rune == 'p' || ev.Rune == 'q' {
			app.PreviewMode = false
			return false, false
		}
	case terminal.KeyUp:
		if app.PreviewScroll > 0 {
			app.PreviewScroll--
		}
	case terminal.KeyDown:
		if app.PreviewScroll < maxScroll {
			app.PreviewScroll++
		}
	}

	if ev.Key == terminal.KeyRune {
		switch ev.Rune {
		case 'j':
			if app.PreviewScroll < maxScroll {
				app.PreviewScroll++
			}
		case 'k':
			if app.PreviewScroll > 0 {
				app.PreviewScroll--
			}
		}
	}

	return false, false
}

// MoveCursor moves cursor with bounds checking and scroll adjustment
func (app *AppState) MoveCursor(delta int) {
	if len(app.PackageList) == 0 {
		app.CursorPos = 0
		app.ScrollOffset = 0
		return
	}
	app.CursorPos += delta
	if app.CursorPos < 0 {
		app.CursorPos = 0
	}
	if app.CursorPos >= len(app.PackageList) {
		app.CursorPos = len(app.PackageList) - 1
	}

	// Adjust scroll
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight
	if visibleRows < 1 {
		visibleRows = 1
	}

	if app.CursorPos < app.ScrollOffset {
		app.ScrollOffset = app.CursorPos
	}
	if app.CursorPos >= app.ScrollOffset+visibleRows {
		app.ScrollOffset = app.CursorPos - visibleRows + 1
	}
}

// CycleGroup cycles through available groups
func (app *AppState) CycleGroup() {
	if len(app.Index.Groups) == 0 {
		return
	}

	if app.ActiveGroup == "" {
		app.GroupIndex = 0
		app.ActiveGroup = app.Index.Groups[0]
	} else {
		app.GroupIndex++
		if app.GroupIndex >= len(app.Index.Groups) {
			app.GroupIndex = -1
			app.ActiveGroup = ""
		} else {
			app.ActiveGroup = app.Index.Groups[app.GroupIndex]
		}
	}

	app.UpdatePackageList()
}

// EnterPreview enters preview mode
func (app *AppState) EnterPreview() {
	app.PreviewFiles = app.ComputeOutputFiles()
	app.PreviewMode = true
	app.PreviewScroll = 0
}
