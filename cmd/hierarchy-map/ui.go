package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// HandleEvent processes a terminal event and returns quit/output flags
func (app *AppState) HandleEvent(ev terminal.Event) (quit, output bool) {
	// Quit is handled in global event loop of main: Ctrl+C, Ctrl+Q to quit
	app.Message = ""

	// Handle help overlay first (for all views)
	if app.Help != nil && app.Help.Visible {
		app.handleHelpEvent(ev)
		return false, false
	}

	// Handle editor events second if visible
	if app.Editor != nil && app.Editor.Visible {
		app.handleEditorEvent(ev)
		return false, false
	}

	// Handle viewer events third if visible
	if app.Viewer != nil && app.Viewer.Visible {
		app.handleViewerEvent(ev)
		return false, false
	}

	if app.InputMode {
		return app.handleInputEvent(ev)
	}

	// Global keybindings
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case '?':
			app.ToggleHelp()
			return false, false
		case 'e':
			app.OpenEditor()
			return false, false
		case 'f':
			app.applyCurrentPaneFilter()
			return false, false
		case '/':
			app.InputMode = true
			app.InputField.Clear()
			return false, false
		case 'r':
			app.ReindexAll()
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
			app.cycleFilterMode()
			return false, false
		case 'c':
			app.Selected = make(map[string]bool)
			app.Message = "cleared selections"
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
		app.FocusPane = (app.FocusPane + 1) % 4
		return false, false

	case terminal.KeyBacktab:
		app.FocusPane = (app.FocusPane + 3) % 4
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

	case terminal.KeyCtrlL:
		app.loadSelectionFromFile()
		return false, false

	case terminal.KeyEscape:
		if app.Filter.HasActiveFilter() {
			app.ClearFilter()
			app.RefreshHierarchyFlat()
			app.Message = "filter cleared"
		}
		return false, false
	}

	// Pane-specific handling
	switch app.FocusPane {
	case PaneHierarchy:
		app.handleHierarchyPaneEvent(ev)
	case PaneTree:
		app.handleTreePaneEvent(ev)
	case PaneDepBy:
		app.handleDetailPaneEvent(ev, app.DepByState)
	case PaneDepOn:
		app.handleDetailPaneEvent(ev, app.DepOnState)
	}

	return false, false
}

// cycleFilterMode advances through filter modes
func (app *AppState) cycleFilterMode() {
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
}

// loadSelectionFromFile loads file selection from the output file
func (app *AppState) loadSelectionFromFile() {
	paths, err := LoadSelectionFile(outputPath, app.Index)
	if err != nil {
		app.Message = fmt.Sprintf("load error: %v", err)
		return
	}

	app.Selected = make(map[string]bool)
	for _, p := range paths {
		app.Selected[p] = true
	}

	app.Message = fmt.Sprintf("loaded %d files from %s", len(paths), outputPath)
}

// handleInputEvent processes events during input mode
func (app *AppState) handleInputEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputField.Clear()
		return false, false

	case terminal.KeyEnter:
		app.InputMode = false
		app.executeSearch(app.InputField.Value())
		app.InputField.Clear()
		return false, false

	default:
		app.InputField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}

	return false, false
}

// --- Hierarchy Pane Event Handling ---

func (app *AppState) handleHierarchyPaneEvent(ev terminal.Event) {
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	switch ev.Key {
	case terminal.KeyUp:
		ui.TreeState.MoveCursor(-1, len(ui.Flat))
	case terminal.KeyDown:
		ui.TreeState.MoveCursor(1, len(ui.Flat))
	case terminal.KeyPageUp:
		ui.TreeState.PageUp(len(ui.Flat))
	case terminal.KeyPageDown:
		ui.TreeState.PageDown(len(ui.Flat))
	case terminal.KeyHome:
		ui.TreeState.JumpStart()
	case terminal.KeyEnd:
		ui.TreeState.JumpEnd(len(ui.Flat))
	case terminal.KeySpace:
		app.toggleHierarchySelection()
	case terminal.KeyLeft:
		app.collapseHierarchyItem(ui)
	case terminal.KeyRight:
		app.expandHierarchyItem(ui)

	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			ui.TreeState.MoveCursor(1, len(ui.Flat))
		case 'k':
			ui.TreeState.MoveCursor(-1, len(ui.Flat))
		case 'h':
			app.collapseHierarchyItem(ui)
		case 'l':
			app.expandHierarchyItem(ui)
		case ' ':
			app.toggleHierarchySelection()
		case 's':
			app.selectAndAdvanceHierarchy()
		case 'a':
			app.selectAllVisibleHierarchyTags()
		case '0':
			ui.TreeState.JumpStart()
		case '$':
			ui.TreeState.JumpEnd(len(ui.Flat))
		case 'H':
			ui.Expansion.CollapseAll()
			app.RefreshHierarchyFlat()
			ui.TreeState.JumpStart()
			app.Message = "collapsed all groups"
		case 'L':
			app.expandAllHierarchyItems(ui)
			app.Message = "expanded all groups"
		}
	}
}

func (app *AppState) collapseHierarchyItem(ui *CategoryUIState) {
	data := ui.CurrentData()
	if data == nil {
		return
	}

	switch data.Type {
	case TagItemTypeCategory:
		key := "c:" + data.Category
		if ui.Expansion.IsExpanded(key) {
			ui.Expansion.Collapse(key)
			app.RefreshHierarchyFlat()
		}
	case TagItemTypeGroup:
		key := "g:" + data.Category + "." + data.Group
		if ui.Expansion.IsExpanded(key) {
			ui.Expansion.Collapse(key)
			app.RefreshHierarchyFlat()
		} else {
			// Navigate to parent category
			for i := ui.TreeState.Cursor - 1; i >= 0; i-- {
				if i < len(ui.Flat) && ui.Flat[i].Type == TagItemTypeCategory && ui.Flat[i].Category == data.Category {
					ui.TreeState.Cursor = i
					ui.TreeState.AdjustScroll(len(ui.Flat))
					break
				}
			}
		}
	case TagItemTypeModule:
		key := fmt.Sprintf("m:%s.%s.%s", data.Category, data.Group, data.Module)
		if ui.Expansion.IsExpanded(key) {
			ui.Expansion.Collapse(key)
			app.RefreshHierarchyFlat()
		} else {
			// Navigate to parent group
			for i := ui.TreeState.Cursor - 1; i >= 0; i-- {
				if i < len(ui.Flat) && ui.Flat[i].Type == TagItemTypeGroup &&
					ui.Flat[i].Category == data.Category && ui.Flat[i].Group == data.Group {
					ui.TreeState.Cursor = i
					ui.TreeState.AdjustScroll(len(ui.Flat))
					break
				}
			}
		}
	case TagItemTypeTag:
		// Navigate to parent (module, group, or category)
		for i := ui.TreeState.Cursor - 1; i >= 0; i-- {
			if i >= len(ui.Flat) {
				continue
			}
			item := ui.Flat[i]
			// 4-level: go to module
			if data.Module != DirectTagsModule && item.Type == TagItemTypeModule &&
				item.Category == data.Category && item.Group == data.Group && item.Module == data.Module {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
			// 3-level: go to group
			if data.Group != DirectTagsGroup && item.Type == TagItemTypeGroup &&
				item.Category == data.Category && item.Group == data.Group {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
			// 2-level: go to category
			if item.Type == TagItemTypeCategory && item.Category == data.Category {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
		}
	}
}

func (app *AppState) expandHierarchyItem(ui *CategoryUIState) {
	data := ui.CurrentData()
	if data == nil {
		return
	}

	switch data.Type {
	case TagItemTypeCategory:
		key := "c:" + data.Category
		if !ui.Expansion.IsExpanded(key) {
			ui.Expansion.Expand(key)
			app.RefreshHierarchyFlat()
		}
	case TagItemTypeGroup:
		key := "g:" + data.Category + "." + data.Group
		if !ui.Expansion.IsExpanded(key) {
			ui.Expansion.Expand(key)
			app.RefreshHierarchyFlat()
		}
	case TagItemTypeModule:
		key := fmt.Sprintf("m:%s.%s.%s", data.Category, data.Group, data.Module)
		if !ui.Expansion.IsExpanded(key) {
			ui.Expansion.Expand(key)
			app.RefreshHierarchyFlat()
		}
	}
}

func (app *AppState) expandAllHierarchyItems(ui *CategoryUIState) {
	for _, cat := range app.CategoryNames {
		ui.Expansion.Expand("c:" + cat)

		catIdx := app.Index.Category(cat)
		if catIdx == nil {
			continue
		}

		for _, group := range catIdx.Groups {
			ui.Expansion.Expand("g:" + cat + "." + group)
		}
		for group, modules := range catIdx.Modules {
			for _, mod := range modules {
				ui.Expansion.Expand(fmt.Sprintf("m:%s.%s.%s", cat, group, mod))
			}
		}
	}

	app.RefreshHierarchyFlat()
}

func (app *AppState) toggleHierarchySelection() {
	ui := app.getCurrentCategoryUI()
	data := ui.CurrentData()
	if data == nil {
		return
	}

	cat := data.Category

	switch data.Type {
	case TagItemTypeCategory:
		// Select/deselect all files in category
		count := 0
		allSelected := true
		for path, fi := range app.Index.Files {
			if fi.HasCategory(cat) {
				if !app.Selected[path] {
					allSelected = false
				}
			}
		}
		for path, fi := range app.Index.Files {
			if fi.HasCategory(cat) {
				if allSelected {
					delete(app.Selected, path)
					count++
				} else if !app.Selected[path] {
					app.Selected[path] = true
					count++
				}
			}
		}
		if allSelected {
			app.Message = fmt.Sprintf("deselected %d files from #%s", count, cat)
		} else {
			app.Message = fmt.Sprintf("selected %d files with #%s", count, cat)
		}
	case TagItemTypeGroup:
		if app.allFilesWithGroupSelected(cat, data.Group) {
			count := app.deselectFilesWithGroup(cat, data.Group)
			app.Message = fmt.Sprintf("deselected %d files from #%s", count, data.Group)
		} else {
			count := app.selectFilesWithGroup(cat, data.Group)
			app.Message = fmt.Sprintf("selected %d files with #%s", count, data.Group)
		}
	case TagItemTypeModule:
		if app.allFilesWithModuleSelected(cat, data.Group, data.Module) {
			count := app.deselectFilesWithModule(cat, data.Group, data.Module)
			app.Message = fmt.Sprintf("deselected %d files from %s", count, data.Module)
		} else {
			count := app.selectFilesWithModule(cat, data.Group, data.Module)
			app.Message = fmt.Sprintf("selected %d files with %s", count, data.Module)
		}
	case TagItemTypeTag:
		if app.allFilesWithTagSelected(cat, data.Group, data.Module, data.Tag) {
			count := app.deselectFilesWithTag(cat, data.Group, data.Module, data.Tag)
			app.Message = fmt.Sprintf("deselected %d files from %s", count, data.Tag)
		} else {
			count := app.selectFilesWithTag(cat, data.Group, data.Module, data.Tag)
			app.Message = fmt.Sprintf("selected %d files with %s", count, data.Tag)
		}
	}
}

func (app *AppState) selectAndAdvanceHierarchy() {
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	data := ui.CurrentData()
	if data == nil {
		return
	}

	cat := data.Category

	switch data.Type {
	case TagItemTypeCategory:
		for path, fi := range app.Index.Files {
			if fi.HasCategory(cat) {
				app.Selected[path] = true
			}
		}
		// Advance to next category
		for i := ui.TreeState.Cursor + 1; i < len(ui.Flat); i++ {
			if ui.Flat[i].Type == TagItemTypeCategory {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
		}
		ui.TreeState.MoveCursor(1, len(ui.Flat))

	case TagItemTypeGroup:
		app.selectFilesWithGroup(cat, data.Group)
		for i := ui.TreeState.Cursor + 1; i < len(ui.Flat); i++ {
			if ui.Flat[i].Type == TagItemTypeGroup || ui.Flat[i].Type == TagItemTypeCategory {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
		}
		ui.TreeState.MoveCursor(1, len(ui.Flat))

	case TagItemTypeModule:
		app.selectFilesWithModule(cat, data.Group, data.Module)
		for i := ui.TreeState.Cursor + 1; i < len(ui.Flat); i++ {
			t := ui.Flat[i].Type
			if t == TagItemTypeModule || t == TagItemTypeGroup || t == TagItemTypeCategory {
				ui.TreeState.Cursor = i
				ui.TreeState.AdjustScroll(len(ui.Flat))
				return
			}
		}
		ui.TreeState.MoveCursor(1, len(ui.Flat))

	case TagItemTypeTag:
		app.selectFilesWithTag(cat, data.Group, data.Module, data.Tag)
		ui.TreeState.MoveCursor(1, len(ui.Flat))
	}
}

func (app *AppState) selectAllVisibleHierarchyTags() {
	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}

	count := 0
	for _, item := range ui.Flat {
		if item.Type == TagItemTypeTag {
			count += app.selectFilesWithTag(item.Category, item.Group, item.Module, item.Tag)
		}
	}
	app.Message = fmt.Sprintf("selected %d files from visible tags", count)
}

// --- Tree Pane Event Handling ---

func (app *AppState) handleTreePaneEvent(ev terminal.Event) {
	if len(app.TreeFlat) == 0 {
		return
	}

	prevCursor := app.TreeState.Cursor

	switch ev.Key {
	case terminal.KeyUp:
		app.TreeState.MoveCursor(-1, len(app.TreeFlat))
	case terminal.KeyDown:
		app.TreeState.MoveCursor(1, len(app.TreeFlat))
	case terminal.KeyPageUp:
		app.TreeState.PageUp(len(app.TreeFlat))
	case terminal.KeyPageDown:
		app.TreeState.PageDown(len(app.TreeFlat))
	case terminal.KeyHome:
		app.TreeState.JumpStart()
	case terminal.KeyEnd:
		app.TreeState.JumpEnd(len(app.TreeFlat))
	case terminal.KeySpace:
		app.toggleTreeSelection()
	case terminal.KeyLeft:
		app.collapseTreeNode()
	case terminal.KeyRight:
		app.expandTreeNode()
	case terminal.KeyEnter:
		if app.TreeState.Cursor < len(app.TreeFlat) {
			node := app.TreeFlat[app.TreeState.Cursor]
			if !node.IsDir {
				app.OpenFileViewer(node.Path)
			}
		}

	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.TreeState.MoveCursor(1, len(app.TreeFlat))
		case 'k':
			app.TreeState.MoveCursor(-1, len(app.TreeFlat))
		case 'h':
			app.collapseTreeNode()
		case 'l':
			app.expandTreeNode()
		case ' ':
			app.toggleTreeSelection()
		case 's':
			app.selectAndAdvanceTree()
		case 'a':
			app.selectAllVisible()
		case '0':
			app.TreeState.JumpStart()
		case '$':
			app.TreeState.JumpEnd(len(app.TreeFlat))
		case 'H':
			collapseAllRecursive(app.TreeRoot)
			app.RefreshTreeFlat()
			app.TreeState.JumpStart()
			app.Message = "collapsed all directories"
		case 'L':
			expandAllRecursive(app.TreeRoot)
			app.RefreshTreeFlat()
			app.Message = "expanded all directories"
		}
	}

	// Refresh detail panes if cursor moved
	if app.TreeState.Cursor != prevCursor {
		app.triggerAnalysis()
		app.refreshDetailPanes()
	}
}

func (app *AppState) collapseTreeNode() {
	if app.TreeState.Cursor >= len(app.TreeFlat) {
		return
	}
	node := app.TreeFlat[app.TreeState.Cursor]

	if node.IsDir && node.Expanded {
		node.Expanded = false
		app.RefreshTreeFlat()
		return
	}

	// MoveEntity to parent
	if node.Parent != nil && node.Parent.Path != "." {
		for i, n := range app.TreeFlat {
			if n == node.Parent {
				app.TreeState.Cursor = i
				app.TreeState.AdjustScroll(len(app.TreeFlat))
				break
			}
		}
	}
}

func (app *AppState) expandTreeNode() {
	if app.TreeState.Cursor >= len(app.TreeFlat) {
		return
	}
	node := app.TreeFlat[app.TreeState.Cursor]

	if node.IsDir && !node.Expanded {
		node.Expanded = true
		app.RefreshTreeFlat()
	}
}

func (app *AppState) toggleTreeSelection() {
	if app.TreeState.Cursor >= len(app.TreeFlat) {
		return
	}
	node := app.TreeFlat[app.TreeState.Cursor]

	if node.IsDir {
		allSelected := true
		var files []string
		collectFiles(node, &files)

		for _, f := range files {
			if !app.Selected[f] {
				allSelected = false
				break
			}
		}

		for _, f := range files {
			if allSelected {
				delete(app.Selected, f)
			} else {
				app.Selected[f] = true
			}
		}
	} else {
		if app.Selected[node.Path] {
			delete(app.Selected, node.Path)
		} else {
			app.Selected[node.Path] = true
		}
	}
}

func (app *AppState) selectAndAdvanceTree() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeState.Cursor]

	if node.IsDir {
		var files []string
		collectFiles(node, &files)
		for _, f := range files {
			app.Selected[f] = true
		}

		// MoveEntity to next sibling directory
		for i := app.TreeState.Cursor + 1; i < len(app.TreeFlat); i++ {
			if app.TreeFlat[i].IsDir && app.TreeFlat[i].Depth == node.Depth {
				app.TreeState.Cursor = i
				app.TreeState.AdjustScroll(len(app.TreeFlat))
				return
			}
		}
		app.TreeState.MoveCursor(1, len(app.TreeFlat))
	} else {
		app.Selected[node.Path] = true
		app.TreeState.MoveCursor(1, len(app.TreeFlat))
	}
}

func (app *AppState) selectAllVisible() {
	for _, node := range app.TreeFlat {
		if !node.IsDir {
			app.Selected[node.Path] = true
		}
	}
	app.Message = "selected all visible files"
}

// --- Detail Pane Event Handling ---

func (app *AppState) handleDetailPaneEvent(ev terminal.Event, state *DetailPaneState) {
	if len(state.FlatItems) == 0 {
		return
	}

	switch ev.Key {
	case terminal.KeyUp:
		state.TreeState.MoveCursor(-1, len(state.FlatItems))
	case terminal.KeyDown:
		state.TreeState.MoveCursor(1, len(state.FlatItems))
	case terminal.KeyPageUp:
		state.TreeState.PageUp(len(state.FlatItems))
	case terminal.KeyPageDown:
		state.TreeState.PageDown(len(state.FlatItems))
	case terminal.KeyHome:
		state.TreeState.JumpStart()
	case terminal.KeyEnd:
		state.TreeState.JumpEnd(len(state.FlatItems))
	case terminal.KeySpace:
		app.toggleDetailSelection(state)
	case terminal.KeyLeft:
		app.collapseDetailItem(state)
	case terminal.KeyRight, terminal.KeyEnter:
		app.expandDetailItem(state)

	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			state.TreeState.MoveCursor(1, len(state.FlatItems))
		case 'k':
			state.TreeState.MoveCursor(-1, len(state.FlatItems))
		case 'h':
			app.collapseDetailItem(state)
		case 'l':
			app.expandDetailItem(state)
		case ' ':
			app.toggleDetailSelection(state)
		case 's':
			app.selectAndAdvanceDetail(state)
		case 'a':
			app.selectAllDetailFiles(state)
		case '0':
			state.TreeState.JumpStart()
		case '$':
			state.TreeState.JumpEnd(len(state.FlatItems))
		case 'H':
			state.Expansion.CollapseAll()
			app.refreshDetailPanes()
			state.TreeState.JumpStart()
		case 'L':
			for _, item := range state.FlatItems {
				if item.IsHeader {
					state.Expansion.Expand(item.Key)
				}
			}
			app.refreshDetailPanes()
		}
	}
}

func (app *AppState) collapseDetailItem(state *DetailPaneState) {
	if state.TreeState.Cursor >= len(state.FlatItems) {
		return
	}
	item := state.FlatItems[state.TreeState.Cursor]

	if item.IsHeader && state.Expansion.IsExpanded(item.Key) {
		state.Expansion.Collapse(item.Key)
		app.refreshDetailPanes()
	} else if item.Level > 0 {
		// MoveEntity to parent header
		for i := state.TreeState.Cursor - 1; i >= 0; i-- {
			if state.FlatItems[i].IsHeader && state.FlatItems[i].Level < item.Level {
				state.TreeState.Cursor = i
				state.TreeState.AdjustScroll(len(state.FlatItems))
				break
			}
		}
	}
}

func (app *AppState) expandDetailItem(state *DetailPaneState) {
	if state.TreeState.Cursor >= len(state.FlatItems) {
		return
	}
	item := state.FlatItems[state.TreeState.Cursor]

	if item.IsHeader && !state.Expansion.IsExpanded(item.Key) {
		state.Expansion.Expand(item.Key)
		app.refreshDetailPanes()
	} else if item.IsFile && item.Path != "" {
		app.navigateTreeToFile(item.Path)
	}
}

func (app *AppState) toggleDetailSelection(state *DetailPaneState) {
	if state.TreeState.Cursor >= len(state.FlatItems) {
		return
	}
	item := state.FlatItems[state.TreeState.Cursor]

	// Symbols and external packages not selectable
	if item.IsSymbol || !item.IsLocal {
		return
	}

	if item.IsFile && item.Path != "" {
		if app.Selected[item.Path] {
			delete(app.Selected, item.Path)
			app.Message = fmt.Sprintf("deselected: %s", filepath.Base(item.Path))
		} else {
			app.Selected[item.Path] = true
			app.Message = fmt.Sprintf("selected: %s", filepath.Base(item.Path))
		}
		return
	}

	if item.IsHeader && item.PkgDir != "" {
		pkg := app.Index.Packages[item.PkgDir]
		if pkg == nil {
			return
		}

		allSelected := true
		for _, fi := range pkg.Files {
			if !app.Selected[fi.Path] {
				allSelected = false
				break
			}
		}

		count := 0
		for _, fi := range pkg.Files {
			if allSelected {
				if app.Selected[fi.Path] {
					delete(app.Selected, fi.Path)
					count++
				}
			} else {
				if !app.Selected[fi.Path] {
					app.Selected[fi.Path] = true
					count++
				}
			}
		}

		if allSelected {
			app.Message = fmt.Sprintf("deselected %d files from %s", count, item.Label)
		} else {
			app.Message = fmt.Sprintf("selected %d files from %s", count, item.Label)
		}
	}
}

func (app *AppState) selectAndAdvanceDetail(state *DetailPaneState) {
	if len(state.FlatItems) == 0 {
		return
	}

	item := state.FlatItems[state.TreeState.Cursor]

	if item.IsSymbol || !item.IsLocal {
		state.TreeState.MoveCursor(1, len(state.FlatItems))
		return
	}

	if item.IsFile && item.Path != "" {
		if !app.Selected[item.Path] {
			app.Selected[item.Path] = true
		}
		state.TreeState.MoveCursor(1, len(state.FlatItems))
		return
	}

	if item.IsHeader && item.PkgDir != "" {
		pkg := app.Index.Packages[item.PkgDir]
		if pkg != nil {
			for _, fi := range pkg.Files {
				app.Selected[fi.Path] = true
			}
		}

		// MoveEntity to next header at same level
		for i := state.TreeState.Cursor + 1; i < len(state.FlatItems); i++ {
			if state.FlatItems[i].IsHeader && state.FlatItems[i].Level == item.Level {
				state.TreeState.Cursor = i
				state.TreeState.AdjustScroll(len(state.FlatItems))
				return
			}
		}
		state.TreeState.Cursor = len(state.FlatItems) - 1
		state.TreeState.AdjustScroll(len(state.FlatItems))
	}
}

func (app *AppState) selectAllDetailFiles(state *DetailPaneState) {
	count := 0
	for _, item := range state.FlatItems {
		if item.IsFile && item.IsLocal && item.Path != "" {
			if !app.Selected[item.Path] {
				app.Selected[item.Path] = true
				count++
			}
		}
	}
	app.Message = fmt.Sprintf("selected %d files", count)
}

// --- Navigation ---

func (app *AppState) navigateTreeToFile(path string) {
	var findAndExpand func(node *TreeNode) bool
	findAndExpand = func(node *TreeNode) bool {
		if node.Path == path {
			return true
		}
		for _, child := range node.Children {
			if findAndExpand(child) {
				if node.IsDir {
					node.Expanded = true
				}
				return true
			}
		}
		return false
	}

	if findAndExpand(app.TreeRoot) {
		app.RefreshTreeFlat()
		for i, n := range app.TreeFlat {
			if n.Path == path {
				app.TreeState.Cursor = i
				app.TreeState.AdjustScroll(len(app.TreeFlat))
				app.FocusPane = PaneTree
				app.Message = fmt.Sprintf("navigated to %s", path)
				app.triggerAnalysis()
				app.refreshDetailPanes()
				return
			}
		}
	}
}

// --- Category Management ---

func (app *AppState) getCurrentCategoryUI() *CategoryUIState {
	if app.HierarchyUI == nil {
		app.HierarchyUI = NewCategoryUIState()
	}
	return app.HierarchyUI
}

// --- Filter ---

func (app *AppState) applyCurrentPaneFilter() {
	switch app.FocusPane {
	case PaneHierarchy:
		app.applyHierarchyPaneFilter()
	case PaneTree:
		app.applyTreePaneFilter()
	case PaneDepBy, PaneDepOn:
		app.Message = "filter not available in dep panes"
	}
}

// --- Refresh Functions ---

func (app *AppState) RefreshTreeFlat() {
	app.TreeFlat = FlattenTree(app.TreeRoot)

	if app.TreeState.Cursor >= len(app.TreeFlat) {
		app.TreeState.Cursor = len(app.TreeFlat) - 1
	}
	if app.TreeState.Cursor < 0 {
		app.TreeState.Cursor = 0
	}
	app.TreeState.AdjustScroll(len(app.TreeFlat))
}

func (app *AppState) RefreshHierarchyFlat() {
	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}

	ui.Flat = nil

	for _, cat := range app.CategoryNames {
		catIdx := app.Index.Category(cat)
		if catIdx == nil {
			continue
		}

		catKey := "c:" + cat
		if _, known := ui.Expansion.State[catKey]; !known {
			ui.Expansion.Expand(catKey)
		}
		catExpanded := ui.Expansion.IsExpanded(catKey)

		// AddEntityAt category node
		ui.Flat = append(ui.Flat, TagItem{
			Type:     TagItemTypeCategory,
			Category: cat,
		})

		if !catExpanded {
			continue
		}

		// Direct category tags (2-level)
		if tags, ok := catIdx.Tags[DirectTagsGroup]; ok {
			if directTags, ok := tags[DirectTagsModule]; ok && len(directTags) > 0 {
				for _, tag := range directTags {
					ui.Flat = append(ui.Flat, TagItem{
						Type:     TagItemTypeTag,
						Category: cat,
						Group:    DirectTagsGroup,
						Module:   DirectTagsModule,
						Tag:      tag,
					})
				}
			}
		}

		// Groups
		for _, group := range catIdx.Groups {
			groupKey := "g:" + cat + "." + group
			if _, known := ui.Expansion.State[groupKey]; !known {
				ui.Expansion.Expand(groupKey)
			}
			groupExpanded := ui.Expansion.IsExpanded(groupKey)

			ui.Flat = append(ui.Flat, TagItem{
				Type:     TagItemTypeGroup,
				Category: cat,
				Group:    group,
			})

			if !groupExpanded {
				continue
			}

			// Direct group tags (3-level)
			if tags, ok := catIdx.Tags[group][DirectTagsModule]; ok {
				for _, tag := range tags {
					ui.Flat = append(ui.Flat, TagItem{
						Type:     TagItemTypeTag,
						Category: cat,
						Group:    group,
						Module:   DirectTagsModule,
						Tag:      tag,
					})
				}
			}

			// Modules
			if modules, ok := catIdx.Modules[group]; ok {
				for _, module := range modules {
					moduleKey := fmt.Sprintf("m:%s.%s.%s", cat, group, module)
					if _, known := ui.Expansion.State[moduleKey]; !known {
						ui.Expansion.Expand(moduleKey)
					}
					moduleExpanded := ui.Expansion.IsExpanded(moduleKey)

					ui.Flat = append(ui.Flat, TagItem{
						Type:     TagItemTypeModule,
						Category: cat,
						Group:    group,
						Module:   module,
					})

					if !moduleExpanded {
						continue
					}

					// Module tags (4-level)
					if tags, ok := catIdx.Tags[group][module]; ok {
						for _, tag := range tags {
							ui.Flat = append(ui.Flat, TagItem{
								Type:     TagItemTypeTag,
								Category: cat,
								Group:    group,
								Module:   module,
								Tag:      tag,
							})
						}
					}
				}
			}
		}
	}

	if ui.TreeState.Cursor >= len(ui.Flat) {
		ui.TreeState.Cursor = len(ui.Flat) - 1
	}
	if ui.TreeState.Cursor < 0 {
		ui.TreeState.Cursor = 0
	}
}

// --- Analysis and Detail Pane Refresh ---

func (app *AppState) triggerAnalysis() {
	if len(app.TreeFlat) == 0 || app.TreeState.Cursor >= len(app.TreeFlat) {
		return
	}
	node := app.TreeFlat[app.TreeState.Cursor]
	if node.IsDir {
		return
	}

	if _, ok := app.DepAnalysisCache[node.Path]; !ok {
		analysis, err := AnalyzeFileDependencies(node.Path, app.Index.ModulePath)
		if err == nil {
			app.DepAnalysisCache[node.Path] = analysis
		}
	}
}

func (app *AppState) refreshDetailPanes() {
	app.rebuildDepByFlat()
	app.rebuildDepOnFlat()
}

func (app *AppState) rebuildDepByFlat() {
	app.DepByState.FlatItems = nil
	pkgDir := app.getCurrentFilePackageDir()
	if pkgDir == "" {
		return
	}

	files := app.Index.ReverseDeps[pkgDir]
	if len(files) == 0 {
		return
	}

	// GetComponent target file's exported definitions for usage highlighting
	var targetDefs map[string]bool
	targetFile := app.getCurrentFileInfo()
	if targetFile != nil && len(targetFile.Definitions) > 0 {
		targetDefs = make(map[string]bool, len(targetFile.Definitions))
		for _, def := range targetFile.Definitions {
			targetDefs[def] = true
		}
	}

	fullImportPath := app.Index.ModulePath
	if pkgDir != "." {
		fullImportPath += "/" + pkgDir
	}

	// Group files by package
	type depFile struct {
		Path     string
		HasUsage bool
	}

	filesByPkg := make(map[string][]*depFile)

	for _, fPath := range files {
		hasUsage := false

		if targetDefs != nil {
			analysis, ok := app.DepAnalysisCache[fPath]
			if !ok {
				a, err := AnalyzeFileDependencies(fPath, app.Index.ModulePath)
				if err == nil {
					analysis = a
					app.DepAnalysisCache[fPath] = a
				}
			}

			if analysis != nil {
				if symbols, ok := analysis.UsedSymbols[fullImportPath]; ok {
					for _, sym := range symbols {
						if targetDefs[sym] {
							hasUsage = true
							break
						}
					}
				}
			}
		}

		dir := filepath.Dir(fPath)
		dir = filepath.ToSlash(dir)
		filesByPkg[dir] = append(filesByPkg[dir], &depFile{Path: fPath, HasUsage: hasUsage})
	}

	// Sort packages
	pkgs := make([]string, 0, len(filesByPkg))
	for p := range filesByPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)

	// Build flat items
	for _, p := range pkgs {
		pkgFiles := filesByPkg[p]

		// Sort files: usage first, then alphabetically
		sort.Slice(pkgFiles, func(i, j int) bool {
			if pkgFiles[i].HasUsage && !pkgFiles[j].HasUsage {
				return true
			}
			if !pkgFiles[i].HasUsage && pkgFiles[j].HasUsage {
				return false
			}
			return pkgFiles[i].Path < pkgFiles[j].Path
		})

		// Default to expanded if not set
		if _, known := app.DepByState.Expansion.State[p]; !known {
			app.DepByState.Expansion.Expand(p)
		}
		isExpanded := app.DepByState.Expansion.IsExpanded(p)

		label := p
		if label == "." {
			label = "(root)"
		}

		app.DepByState.FlatItems = append(app.DepByState.FlatItems, DetailItem{
			Level:    0,
			Label:    label,
			Key:      p,
			IsHeader: true,
			Expanded: isExpanded,
			IsLocal:  true,
			PkgDir:   p,
		})

		if isExpanded {
			for _, f := range pkgFiles {
				app.DepByState.FlatItems = append(app.DepByState.FlatItems, DetailItem{
					Level:    1,
					Label:    filepath.Base(f.Path),
					Key:      f.Path,
					IsFile:   true,
					Path:     f.Path,
					PkgDir:   p,
					IsLocal:  true,
					HasUsage: f.HasUsage,
				})
			}
		}
	}
}

func (app *AppState) rebuildDepOnFlat() {
	app.DepOnState.FlatItems = nil
	fi := app.getCurrentFileInfo()
	if fi == nil {
		return
	}

	analysis := app.DepAnalysisCache[fi.Path]
	if analysis == nil {
		return
	}

	// Sort import paths
	importPaths := make([]string, 0, len(analysis.UsedSymbols))
	for p := range analysis.UsedSymbols {
		importPaths = append(importPaths, p)
	}
	sort.Strings(importPaths)

	for _, path := range importPaths {
		isLocal := strings.HasPrefix(path, app.Index.ModulePath)

		var pkgDir string
		if isLocal {
			if path == app.Index.ModulePath {
				pkgDir = "."
			} else {
				pkgDir = strings.TrimPrefix(path, app.Index.ModulePath+"/")
			}
		}

		// Default to expanded if not set
		if _, known := app.DepOnState.Expansion.State[path]; !known {
			app.DepOnState.Expansion.Expand(path)
		}
		isExpanded := app.DepOnState.Expansion.IsExpanded(path)

		dispName := path
		if isLocal {
			dispName = strings.TrimPrefix(path, app.Index.ModulePath+"/")
			if dispName == "" {
				dispName = "(root)"
			}
		}

		symbols := analysis.UsedSymbols[path]
		hasSymbols := len(symbols) > 0 && isLocal

		app.DepOnState.FlatItems = append(app.DepOnState.FlatItems, DetailItem{
			Level:    0,
			Label:    dispName,
			Key:      path,
			IsHeader: hasSymbols,
			Expanded: isExpanded,
			IsLocal:  isLocal,
			PkgDir:   pkgDir,
		})

		if isExpanded && hasSymbols {
			for _, sym := range symbols {
				app.DepOnState.FlatItems = append(app.DepOnState.FlatItems, DetailItem{
					Level:    1,
					Label:    sym,
					Key:      path + "." + sym,
					IsSymbol: true,
					IsLocal:  true,
				})
			}
		}
	}
}