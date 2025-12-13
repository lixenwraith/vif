package main

import (
	"fmt"
	"os/exec"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// HandleEvent routes keyboard events to appropriate handler
func (app *AppState) HandleEvent(ev terminal.Event) (quit, output bool) {
	app.Message = ""

	// Global quit from any view
	if ev.Key == terminal.KeyCtrlQ {
		return true, false
	}

	// Help overlay takes priority
	if app.HelpMode {
		app.HandleHelpEvent(ev)
		return false, false
	}

	if app.DiveMode {
		app.HandleDiveEvent(ev)
		return false, false
	}

	if app.MindmapMode {
		app.HandleMindmapEvent(ev)
		return false, false
	}

	if app.PreviewMode {
		return app.handlePreviewEvent(ev)
	}

	if app.EditMode {
		app.HandleEditEvent(ev)
		return false, false
	}

	if app.InputMode {
		return app.handleInputEvent(ev)
	}

	// Two-key command sequence handling
	if app.CommandPending != 0 {
		return app.handleCommandSequence(ev)
	}

	// Global keys
	switch ev.Key {
	case terminal.KeyCtrlY:
		app.copyOutputToClipboard()
		return false, false

	case terminal.KeyRune:
		switch ev.Rune {
		case '?':
			app.HelpMode = true
			return false, false
		case 'f', 'i':
			// Start two-key filter sequence
			app.CommandPending = ev.Rune
			app.Message = fmt.Sprintf("-%c-", ev.Rune)
			return false, false
		case '/':
			app.InputMode = true
			app.InputBuffer = ""
			app.SearchType = SearchTypeContent
			return false, false
		case 'r':
			app.ReindexAll()
			return false, false
		case 'e':
			app.EnterEditMode()
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
			switch app.Filter.Mode {
			case FilterOR:
				app.Filter.Mode = FilterAND
				app.Message = "filter mode: AND"
			case FilterAND:
				app.Filter.Mode = FilterNOT
				app.Message = "filter mode: NOT"
			case FilterNOT:
				app.Filter.Mode = FilterXOR
				app.Message = "filter mode: XOR"
			case FilterXOR:
				app.Filter.Mode = FilterOR
				app.Message = "filter mode: OR"
			}
			return false, false
		case 'c':
			app.Selected = make(map[string]bool)
			app.Message = "cleared selections"
			return false, false
		case 'p':
			app.EnterPreview()
			return false, false
		case 'F':
			if app.Filter.HasActiveFilter() {
				count := app.selectFilteredFiles()
				app.Message = fmt.Sprintf("selected %d filtered files", count)
			} else {
				app.Message = "no filter active"
			}
			return false, false
		}

	case terminal.KeyTab:
		switch app.FocusPane {
		case PaneLeft:
			app.FocusPane = PaneCenter
		case PaneCenter:
			app.FocusPane = PaneRight
		case PaneRight:
			app.FocusPane = PaneLeft
		}
		return false, false

	case terminal.KeyBacktab:
		switch app.FocusPane {
		case PaneLeft:
			app.FocusPane = PaneRight
		case PaneCenter:
			app.FocusPane = PaneLeft
		case PaneRight:
			app.FocusPane = PaneCenter
		}
		return false, false

	case terminal.KeyEnter:
		app.EnterMindmap()
		return false, false

	case terminal.KeyCtrlS:
		files := app.ComputeOutputFiles()
		if len(files) == 0 {
			app.Message = "no files to output"
			return false, false
		}
		err := WriteOutputFile(outputPath, files)
		if err != nil {
			app.Message = fmt.Sprintf("write error: %v", err)
		} else {
			app.Message = fmt.Sprintf("wrote %d files to %s", len(files), outputPath)
		}
		return false, false

	case terminal.KeyEscape:
		if app.Filter.HasActiveFilter() {
			app.ClearFilter()
			app.RefreshFocusFlat()
			app.RefreshInteractFlat()
			app.Message = "filter cleared"
		}
		return false, false
	}

	switch app.FocusPane {
	case PaneLeft:
		return app.handleTreePaneEvent(ev)
	case PaneCenter:
		return app.handleFocusPaneEvent(ev)
	case PaneRight:
		return app.handleInteractPaneEvent(ev)
	}
	return false, false
}

// applyInitialCollapsedState collapses all panes for fresh start
func (app *AppState) applyInitialCollapsedState() {
	// Collapse tree directories
	collapseAllRecursive(app.TreeRoot)
	app.RefreshTreeFlat()
	app.TreeCursor = 0
	app.TreeScroll = 0

	// Collapse focus groups
	for _, group := range app.Index.FocusGroups {
		app.GroupExpanded[group] = false
	}
	app.RefreshFocusFlat()
	app.TagCursor = 0
	app.TagScroll = 0

	// Collapse interact groups
	for _, group := range app.Index.InteractGroups {
		app.InteractGroupExpanded[group] = false
	}
	app.RefreshInteractFlat()
	app.InteractCursor = 0
	app.InteractScroll = 0
}

// handleCommandSequence processes second key of two-key filter command
func (app *AppState) handleCommandSequence(ev terminal.Event) (quit, output bool) {
	pending := app.CommandPending
	app.CommandPending = 0

	if ev.Key == terminal.KeyEscape {
		app.Message = ""
		return false, false
	}

	if ev.Key != terminal.KeyRune {
		app.Message = "invalid sequence"
		return false, false
	}

	// 'ff' and 'if' - toggle filter on cursor item
	if ev.Rune == 'f' {
		switch pending {
		case 'f':
			app.applyCurrentPaneFilter()
			return false, false
		case 'i':
			app.applyCurrentPaneFilter()
			return false, false
		}
	}

	// Determine category from first key
	switch pending {
	case 'f':
		app.SearchCategory = SearchCategoryFocus
	case 'i':
		app.SearchCategory = SearchCategoryInteract
	default:
		app.Message = "invalid sequence"
		return false, false
	}

	// Determine type from second key
	switch ev.Rune {
	case 'g':
		app.SearchType = SearchTypeGroups
	case 't':
		app.SearchType = SearchTypeTags
	default:
		app.Message = "invalid sequence"
		return false, false
	}

	// Enter input mode for filter query
	app.InputMode = true
	app.InputBuffer = ""

	categoryName := "focus"
	if app.SearchCategory == SearchCategoryInteract {
		categoryName = "interact"
	}
	typeName := "tags"
	if app.SearchType == SearchTypeGroups {
		typeName = "groups"
	}
	app.Message = fmt.Sprintf("filter %s %s:", categoryName, typeName)

	return false, false
}

// applyCurrentPaneFilter applies filter toggle based on active pane
func (app *AppState) applyCurrentPaneFilter() {
	switch app.FocusPane {
	case PaneLeft:
		app.applyTreePaneFilter()
	case PaneCenter:
		app.applyFocusPaneFilter()
	case PaneRight:
		app.applyInteractPaneFilter()
	}
}

// handleTreePaneEvent processes input when tree pane focused
func (app *AppState) handleTreePaneEvent(ev terminal.Event) (quit, output bool) {
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
		case '0':
			app.jumpTreeToStart()
		case '$':
			app.jumpTreeToEnd()
		case 'H':
			app.collapseAllDirs()
		case 'L':
			app.expandAllDirs()
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
	case terminal.KeyHome:
		app.jumpTreeToStart()
	case terminal.KeyEnd:
		app.jumpTreeToEnd()
	}

	return false, false
}

// handleFocusPaneEvent processes input when focus pane is active
func (app *AppState) handleFocusPaneEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveTagCursor(1)
		case 'k':
			app.moveTagCursor(-1)
		case 'h':
			app.collapseCurrentGroup()
		case 'l':
			app.expandCurrentGroup()
		case ' ':
			app.toggleTagSelection()
		case 'a':
			app.selectAllVisibleTags()
		case '0':
			app.jumpTagToStart()
		case '$':
			app.jumpTagToEnd()
		case 'H':
			app.collapseAllGroups()
		case 'L':
			app.expandAllGroups()
		}

	case terminal.KeyUp:
		app.moveTagCursor(-1)
	case terminal.KeyDown:
		app.moveTagCursor(1)
	case terminal.KeyLeft:
		app.collapseCurrentGroup()
	case terminal.KeyRight:
		app.expandCurrentGroup()
	case terminal.KeySpace:
		app.toggleTagSelection()
	case terminal.KeyPageUp:
		app.pageTagCursor(-1)
	case terminal.KeyPageDown:
		app.pageTagCursor(1)
	case terminal.KeyHome:
		app.jumpTagToStart()
	case terminal.KeyEnd:
		app.jumpTagToEnd()
	}

	return false, false
}

// handleInteractPaneEvent processes input when interact pane is active
func (app *AppState) handleInteractPaneEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveInteractCursor(1)
		case 'k':
			app.moveInteractCursor(-1)
		case 'h':
			app.collapseCurrentInteractGroup()
		case 'l':
			app.expandCurrentInteractGroup()
		case ' ':
			app.toggleInteractSelection()
		case 'a':
			app.selectAllVisibleInteractTags()
		case '0':
			app.jumpInteractToStart()
		case '$':
			app.jumpInteractToEnd()
		case 'H':
			app.collapseAllInteractGroups()
		case 'L':
			app.expandAllInteractGroups()
		}

	case terminal.KeyUp:
		app.moveInteractCursor(-1)
	case terminal.KeyDown:
		app.moveInteractCursor(1)
	case terminal.KeyLeft:
		app.collapseCurrentInteractGroup()
	case terminal.KeyRight:
		app.expandCurrentInteractGroup()
	case terminal.KeySpace:
		app.toggleInteractSelection()
	case terminal.KeyPageUp:
		app.pageInteractCursor(-1)
	case terminal.KeyPageDown:
		app.pageInteractCursor(1)
	case terminal.KeyHome:
		app.jumpInteractToStart()
	case terminal.KeyEnd:
		app.jumpInteractToEnd()
	}

	return false, false
}

// jumpTreeToStart moves tree cursor to first item
func (app *AppState) jumpTreeToStart() {
	if len(app.TreeFlat) == 0 {
		return
	}
	app.TreeCursor = 0
	app.TreeScroll = 0
}

// jumpTreeToEnd moves tree cursor to last item
func (app *AppState) jumpTreeToEnd() {
	if len(app.TreeFlat) == 0 {
		return
	}
	app.TreeCursor = len(app.TreeFlat) - 1
	app.moveTreeCursor(0)
}

// collapseAllDirs collapses all expanded directories
func (app *AppState) collapseAllDirs() {
	collapseAllRecursive(app.TreeRoot)
	app.RefreshTreeFlat()
	if app.TreeCursor >= len(app.TreeFlat) {
		app.TreeCursor = len(app.TreeFlat) - 1
	}
	if app.TreeCursor < 0 {
		app.TreeCursor = 0
	}
	app.moveTreeCursor(0)
	app.Message = "collapsed all directories"
}

// expandAllDirs expands all directories in tree
func (app *AppState) expandAllDirs() {
	expandAllRecursive(app.TreeRoot)
	app.RefreshTreeFlat()
	app.moveTreeCursor(0)
	app.Message = "expanded all directories"
}

// jumpTagToStart moves tag cursor to first item
func (app *AppState) jumpTagToStart() {
	if len(app.TagFlat) == 0 {
		return
	}
	app.TagCursor = 0
	app.TagScroll = 0
}

// jumpTagToEnd moves tag cursor to last item
func (app *AppState) jumpTagToEnd() {
	if len(app.TagFlat) == 0 {
		return
	}
	app.TagCursor = len(app.TagFlat) - 1
	app.moveTagCursor(0)
}

// collapseAllGroups collapses all expanded tag groups
func (app *AppState) collapseAllGroups() {
	for _, group := range app.Index.FocusGroups {
		app.GroupExpanded[group] = false
	}
	app.RefreshFocusFlat()
	if app.TagCursor >= len(app.TagFlat) {
		app.TagCursor = len(app.TagFlat) - 1
	}
	if app.TagCursor < 0 {
		app.TagCursor = 0
	}
	app.moveTagCursor(0)
	app.Message = "collapsed all groups"
}

// expandAllGroups expands all tag groups
func (app *AppState) expandAllGroups() {
	for _, group := range app.Index.FocusGroups {
		app.GroupExpanded[group] = true
	}
	app.RefreshFocusFlat()
	app.moveTagCursor(0)
	app.Message = "expanded all groups"
}

// collapseCurrentGroup collapses group at or containing cursor
func (app *AppState) collapseCurrentGroup() {
	if len(app.TagFlat) == 0 {
		return
	}
	item := app.TagFlat[app.TagCursor]
	group := item.Group

	if app.GroupExpanded[group] {
		app.GroupExpanded[group] = false
		app.RefreshFocusFlat()
		// Move cursor to group header if we were on a tag
		if !item.IsGroup {
			for i, ti := range app.TagFlat {
				if ti.IsGroup && ti.Group == group {
					app.TagCursor = i
					break
				}
			}
		}
		app.moveTagCursor(0)
	}
}

// expandCurrentGroup expands group at cursor if collapsed
func (app *AppState) expandCurrentGroup() {
	if len(app.TagFlat) == 0 {
		return
	}
	item := app.TagFlat[app.TagCursor]
	if !item.IsGroup {
		return
	}

	if !app.GroupExpanded[item.Group] {
		app.GroupExpanded[item.Group] = true
		app.RefreshFocusFlat()
		app.moveTagCursor(0)
	}
}

// pageTreeCursor moves tree cursor by half-page
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

// pageTagCursor moves tag cursor by half-page
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

// handleInputEvent processes search input mode keystrokes
func (app *AppState) handleInputEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputBuffer = ""
		return false, false

	case terminal.KeyEnter:
		app.InputMode = false
		app.executeSearch(app.InputBuffer)
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

// handlePreviewEvent processes preview mode keystrokes
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

// moveTreeCursor moves tree cursor with scroll adjustment
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

// moveTagCursor moves tag cursor with scroll adjustment
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

// collapseNode collapses current directory or navigates to parent
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

// expandNode expands directory at cursor
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

// toggleTreeSelection toggles selection of file or directory contents
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

// selectAllVisible selects all files in flattened tree
func (app *AppState) selectAllVisible() {
	for _, node := range app.TreeFlat {
		if !node.IsDir {
			app.Selected[node.Path] = true
		}
	}
	app.Message = "selected all visible files"
}

// selectAllVisibleTags selects files matching filter or all tagged
func (app *AppState) selectAllVisibleTags() {
	count := 0

	if app.Filter.HasActiveFilter() {
		for path := range app.Filter.FilteredPaths {
			if !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	} else {
		for path, fi := range app.Index.Files {
			if len(fi.Focus) > 0 && !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	}

	app.Message = fmt.Sprintf("selected %d files", count)
}

// toggleTagSelection toggles selection for tag or group at cursor
func (app *AppState) toggleTagSelection() {
	if len(app.TagFlat) == 0 {
		return
	}

	item := app.TagFlat[app.TagCursor]

	if item.IsGroup {
		// Toggle all files in group
		if app.allFilesWithGroupSelected(CategoryFocus, item.Group) {
			count := app.deselectFilesWithGroup(CategoryFocus, item.Group)
			app.Message = fmt.Sprintf("deselected %d files from #%s", count, item.Group)
		} else {
			count := app.selectFilesWithGroup(CategoryFocus, item.Group)
			app.Message = fmt.Sprintf("selected %d files with #%s", count, item.Group)
		}
	} else {
		// Toggle all files with this tag
		if app.allFilesWithTagSelected(CategoryFocus, item.Group, item.Tag) {
			count := app.deselectFilesWithTag(CategoryFocus, item.Group, item.Tag)
			app.Message = fmt.Sprintf("deselected %d files from #%s{%s}", count, item.Group, item.Tag)
		} else {
			count := app.selectFilesWithTag(CategoryFocus, item.Group, item.Tag)
			app.Message = fmt.Sprintf("selected %d files with #%s{%s}", count, item.Group, item.Tag)
		}
	}
}

// EnterPreview initializes preview mode with computed output files
func (app *AppState) EnterPreview() {
	app.PreviewFiles = app.ComputeOutputFiles()
	app.PreviewMode = true
	app.PreviewScroll = 0
}

// moveInteractCursor moves interact cursor with scroll adjustment
func (app *AppState) moveInteractCursor(delta int) {
	if len(app.InteractFlat) == 0 {
		return
	}

	app.InteractCursor += delta
	if app.InteractCursor < 0 {
		app.InteractCursor = 0
	}
	if app.InteractCursor >= len(app.InteractFlat) {
		app.InteractCursor = len(app.InteractFlat) - 1
	}

	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if app.InteractCursor < app.InteractScroll {
		app.InteractScroll = app.InteractCursor
	}
	if app.InteractCursor >= app.InteractScroll+visibleRows {
		app.InteractScroll = app.InteractCursor - visibleRows + 1
	}
}

// pageInteractCursor moves interact cursor by half-page
func (app *AppState) pageInteractCursor(direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveInteractCursor(delta)
}

// jumpInteractToStart moves interact cursor to first item
func (app *AppState) jumpInteractToStart() {
	if len(app.InteractFlat) == 0 {
		return
	}
	app.InteractCursor = 0
	app.InteractScroll = 0
}

// jumpInteractToEnd moves interact cursor to last item
func (app *AppState) jumpInteractToEnd() {
	if len(app.InteractFlat) == 0 {
		return
	}
	app.InteractCursor = len(app.InteractFlat) - 1
	app.moveInteractCursor(0)
}

// collapseCurrentInteractGroup collapses interact group at cursor
func (app *AppState) collapseCurrentInteractGroup() {
	if len(app.InteractFlat) == 0 {
		return
	}
	item := app.InteractFlat[app.InteractCursor]
	group := item.Group

	if app.InteractGroupExpanded[group] {
		app.InteractGroupExpanded[group] = false
		app.RefreshInteractFlat()
		if !item.IsGroup {
			for i, ti := range app.InteractFlat {
				if ti.IsGroup && ti.Group == group {
					app.InteractCursor = i
					break
				}
			}
		}
		app.moveInteractCursor(0)
	}
}

// expandCurrentInteractGroup expands interact group at cursor
func (app *AppState) expandCurrentInteractGroup() {
	if len(app.InteractFlat) == 0 {
		return
	}
	item := app.InteractFlat[app.InteractCursor]
	if !item.IsGroup {
		return
	}

	if !app.InteractGroupExpanded[item.Group] {
		app.InteractGroupExpanded[item.Group] = true
		app.RefreshInteractFlat()
		app.moveInteractCursor(0)
	}
}

// collapseAllInteractGroups collapses all interact groups
func (app *AppState) collapseAllInteractGroups() {
	for _, group := range app.Index.InteractGroups {
		app.InteractGroupExpanded[group] = false
	}
	app.RefreshInteractFlat()
	if app.InteractCursor >= len(app.InteractFlat) {
		app.InteractCursor = len(app.InteractFlat) - 1
	}
	if app.InteractCursor < 0 {
		app.InteractCursor = 0
	}
	app.moveInteractCursor(0)
	app.Message = "collapsed all interact groups"
}

// expandAllInteractGroups expands all interact groups
func (app *AppState) expandAllInteractGroups() {
	for _, group := range app.Index.InteractGroups {
		app.InteractGroupExpanded[group] = true
	}
	app.RefreshInteractFlat()
	app.moveInteractCursor(0)
	app.Message = "expanded all interact groups"
}

// toggleInteractSelection toggles selection for interact tag/group at cursor
func (app *AppState) toggleInteractSelection() {
	if len(app.InteractFlat) == 0 {
		return
	}

	item := app.InteractFlat[app.InteractCursor]

	if item.IsGroup {
		if app.allFilesWithGroupSelected(CategoryInteract, item.Group) {
			count := app.deselectFilesWithGroup(CategoryInteract, item.Group)
			app.Message = fmt.Sprintf("deselected %d files from #%s", count, item.Group)
		} else {
			count := app.selectFilesWithGroup(CategoryInteract, item.Group)
			app.Message = fmt.Sprintf("selected %d files with #%s", count, item.Group)
		}
	} else {
		if app.allFilesWithTagSelected(CategoryInteract, item.Group, item.Tag) {
			count := app.deselectFilesWithTag(CategoryInteract, item.Group, item.Tag)
			app.Message = fmt.Sprintf("deselected %d files from #%s{%s}", count, item.Group, item.Tag)
		} else {
			count := app.selectFilesWithTag(CategoryInteract, item.Group, item.Tag)
			app.Message = fmt.Sprintf("selected %d files with #%s{%s}", count, item.Group, item.Tag)
		}
	}
}

// selectAllVisibleInteractTags selects files matching interact filter or all
func (app *AppState) selectAllVisibleInteractTags() {
	count := 0

	if app.Filter.HasActiveFilter() {
		for path := range app.Filter.FilteredPaths {
			if !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	} else {
		for path, fi := range app.Index.Files {
			if len(fi.Interact) > 0 && !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	}

	app.Message = fmt.Sprintf("selected %d files", count)
}

// copyOutputToClipboard pipes computed output files to wl-copy
func (app *AppState) copyOutputToClipboard() {
	files := app.ComputeOutputFiles()
	if len(files) == 0 {
		app.Message = "no files to copy"
		return
	}

	cmd := exec.Command("wl-copy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return // Silent fail
	}

	if err := cmd.Start(); err != nil {
		return // Silent fail
	}

	for _, f := range files {
		fmt.Fprintf(stdin, "./%s\n", f)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return // Silent fail
	}

	app.Message = fmt.Sprintf("copied %d files to clipboard", len(files))
}