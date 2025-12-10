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

	// Global keys
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			return true, false
		case '/':
			if app.RgAvailable {
				app.InputMode = true
				app.InputBuffer = ""
			} else {
				app.Message = "ripgrep (rg) not found"
			}
			return false, false
		case 'd':
			app.ExpandDeps = !app.ExpandDeps
			if app.ExpandDeps {
				app.Message = "dependency expansion ON"
			} else {
				app.Message = "dependency expansion OFF"
			}
			return false, false
		case '+', '=':
			if app.DepthLimit < 5 {
				app.DepthLimit++
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
			return false, false
		case '-':
			if app.DepthLimit > 1 {
				app.DepthLimit--
				app.Message = fmt.Sprintf("depth limit: %d", app.DepthLimit)
			}
			return false, false
		case 'm':
			if app.Filter.Mode == FilterOR {
				app.Filter.Mode = FilterAND
				app.Message = "filter mode: AND"
			} else {
				app.Filter.Mode = FilterOR
				app.Message = "filter mode: OR"
			}
			return false, false
		case 'c':
			app.Selected = make(map[string]bool)
			app.Filter = NewFilterState()
			app.RefreshTagFlat()
			app.Message = "cleared all selections"
			return false, false
		case 'p':
			app.EnterPreview()
			return false, false
		}

	case terminal.KeyTab:
		if app.FocusPane == PaneLeft {
			app.FocusPane = PaneRight
		} else {
			app.FocusPane = PaneLeft
		}
		return false, false

	case terminal.KeyEnter:
		// Write output but stay in app
		files := app.ComputeOutputFiles()
		if len(files) == 0 {
			app.Message = "no files to output"
			return false, false
		}
		err := WriteOutputFile(outputPath, files)
		if err != nil {
			app.Message = fmt.Sprintf("write error: %v", err)
		} else {
			app.Message = fmt.Sprintf("wrote %d files to %s (press q to quit)", len(files), outputPath)
		}
		return false, false

	case terminal.KeyEscape:
		if app.Filter.Keyword != "" || app.Filter.HasSelectedTags() {
			app.Filter = NewFilterState()
			app.RefreshTagFlat()
			app.Message = "filters cleared"
		}
		return false, false
	}

	// Pane-specific handling
	if app.FocusPane == PaneLeft {
		return app.handleLeftPaneEvent(ev)
	}
	return app.handleRightPaneEvent(ev)
}

func (app *AppState) handleLeftPaneEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveTreeCursor(1)
		case 'k':
			app.moveTreeCursor(-1)
		case 'h':
			app.collapseNode()
		case 'l':
			app.expandNode()
		case ' ':
			app.toggleTreeSelection()
		case 'a':
			app.selectAllVisible()
		}

	case terminal.KeyUp:
		app.moveTreeCursor(-1)
	case terminal.KeyDown:
		app.moveTreeCursor(1)
	case terminal.KeyLeft:
		app.collapseNode()
	case terminal.KeyRight:
		app.expandNode()
	case terminal.KeySpace:
		app.toggleTreeSelection()
	case terminal.KeyPageUp:
		app.pageTreeCursor(-1)
	case terminal.KeyPageDown:
		app.pageTreeCursor(1)
	}

	return false, false
}

func (app *AppState) handleRightPaneEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveTagCursor(1)
		case 'k':
			app.moveTagCursor(-1)
		case ' ':
			app.toggleTagSelection()
		}

	case terminal.KeyUp:
		app.moveTagCursor(-1)
	case terminal.KeyDown:
		app.moveTagCursor(1)
	case terminal.KeySpace:
		app.toggleTagSelection()
	case terminal.KeyPageUp:
		app.pageTagCursor(-1)
	case terminal.KeyPageDown:
		app.pageTagCursor(1)
	}

	return false, false
}

// pageTreeCursor moves cursor by page in left pane
func (app *AppState) pageTreeCursor(direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	// Move by half page for smoother navigation
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveTreeCursor(delta)
}

// pageTagCursor moves cursor by page in right pane
func (app *AppState) pageTagCursor(direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	// Move by half page for smoother navigation
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveTagCursor(delta)
}

func (app *AppState) handleInputEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputBuffer = ""
		return false, false

	case terminal.KeyEnter:
		app.InputMode = false
		if app.InputBuffer != "" {
			app.Filter.Keyword = app.InputBuffer
			matches, err := SearchKeyword(".", app.Filter.Keyword, app.Filter.CaseSensitive)
			if err != nil {
				app.Message = "search error: " + err.Error()
				app.Filter.Keyword = ""
			} else {
				app.Filter.KeywordMatch = make(map[string]bool)
				for _, m := range matches {
					app.Filter.KeywordMatch[m] = true
				}
				app.Message = fmt.Sprintf("found %d files", len(matches))
			}
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

// moveTreeCursor moves cursor in left pane with scrolling
func (app *AppState) moveTreeCursor(delta int) {
	if len(app.TreeFlat) == 0 {
		return
	}

	app.TreeCursor += delta
	if app.TreeCursor < 0 {
		app.TreeCursor = 0
	}
	if app.TreeCursor >= len(app.TreeFlat) {
		app.TreeCursor = len(app.TreeFlat) - 1
	}

	// Adjust scroll
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if app.TreeCursor < app.TreeScroll {
		app.TreeScroll = app.TreeCursor
	}
	if app.TreeCursor >= app.TreeScroll+visibleRows {
		app.TreeScroll = app.TreeCursor - visibleRows + 1
	}
}

// moveTagCursor moves cursor in right pane with scrolling
func (app *AppState) moveTagCursor(delta int) {
	if len(app.TagFlat) == 0 {
		return
	}

	app.TagCursor += delta
	if app.TagCursor < 0 {
		app.TagCursor = 0
	}
	if app.TagCursor >= len(app.TagFlat) {
		app.TagCursor = len(app.TagFlat) - 1
	}

	// Adjust scroll
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if app.TagCursor < app.TagScroll {
		app.TagScroll = app.TagCursor
	}
	if app.TagCursor >= app.TagScroll+visibleRows {
		app.TagScroll = app.TagCursor - visibleRows + 1
	}
}

// collapseNode collapses current directory or moves to parent
func (app *AppState) collapseNode() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]

	if node.IsDir && node.Expanded {
		node.Expanded = false
		app.RefreshTreeFlat()
		return
	}

	// Move to parent
	if node.Parent != nil && node.Parent.Path != "." {
		for i, n := range app.TreeFlat {
			if n == node.Parent {
				app.TreeCursor = i
				app.moveTreeCursor(0) // Adjust scroll
				break
			}
		}
	}
}

// expandNode expands current directory
func (app *AppState) expandNode() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]

	if node.IsDir && !node.Expanded {
		node.Expanded = true
		app.RefreshTreeFlat()
	}
}

// toggleTreeSelection toggles selection of current item
func (app *AppState) toggleTreeSelection() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]

	if node.IsDir {
		// Toggle all files in directory
		allSelected := true
		var files []string
		collectFiles(node, &files)

		for _, f := range files {
			if !app.Selected[f] {
				allSelected = false
				break
			}
		}

		// If all selected, deselect all; otherwise select all
		for _, f := range files {
			if allSelected {
				delete(app.Selected, f)
			} else {
				app.Selected[f] = true
			}
		}
	} else {
		// Toggle single file
		if app.Selected[node.Path] {
			delete(app.Selected, node.Path)
		} else {
			app.Selected[node.Path] = true
		}
	}
}

// collectFiles recursively collects all file paths under a node
func collectFiles(node *TreeNode, files *[]string) {
	if !node.IsDir {
		*files = append(*files, node.Path)
		return
	}

	for _, child := range node.Children {
		collectFiles(child, files)
	}
}

// selectAllVisible selects all visible files in tree
func (app *AppState) selectAllVisible() {
	for _, node := range app.TreeFlat {
		if !node.IsDir {
			app.Selected[node.Path] = true
		}
	}
	app.Message = "selected all visible files"
}

// toggleTagSelection toggles tag selection in right pane
func (app *AppState) toggleTagSelection() {
	if len(app.TagFlat) == 0 {
		return
	}

	item := app.TagFlat[app.TagCursor]

	if item.IsGroup {
		// Toggle all tags in group
		if app.Filter.SelectedTags[item.Group] == nil {
			app.Filter.SelectedTags[item.Group] = make(map[string]bool)
		}

		// Check if all selected
		allSelected := true
		if tags, ok := app.Index.AllTags[item.Group]; ok {
			for _, tag := range tags {
				if !app.Filter.SelectedTags[item.Group][tag] {
					allSelected = false
					break
				}
			}

			// Toggle
			for _, tag := range tags {
				app.Filter.SelectedTags[item.Group][tag] = !allSelected
			}
		}
	} else {
		// Toggle single tag
		if app.Filter.SelectedTags[item.Group] == nil {
			app.Filter.SelectedTags[item.Group] = make(map[string]bool)
		}
		app.Filter.SelectedTags[item.Group][item.Tag] = !app.Filter.SelectedTags[item.Group][item.Tag]
	}

	app.RefreshTagFlat()
}

// EnterPreview enters preview mode
func (app *AppState) EnterPreview() {
	app.PreviewFiles = app.ComputeOutputFiles()
	app.PreviewMode = true
	app.PreviewScroll = 0
}