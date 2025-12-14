package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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
		case '[':
			app.switchCategory(-1)
			return false, false
		case ']':
			app.switchCategory(1)
			return false, false
		case 'f':
			app.applyCurrentPaneFilter()
			return false, false
		case '/':
			app.InputMode = true
			app.InputBuffer = ""
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
		case PaneLixen:
			app.FocusPane = PaneTree
		case PaneTree:
			app.FocusPane = PaneDepBy
		case PaneDepBy:
			app.FocusPane = PaneDepOn
		case PaneDepOn:
			app.FocusPane = PaneLixen
		}
		return false, false

	case terminal.KeyBacktab:
		switch app.FocusPane {
		case PaneLixen:
			app.FocusPane = PaneDepOn
		case PaneTree:
			app.FocusPane = PaneLixen
		case PaneDepBy:
			app.FocusPane = PaneTree
		case PaneDepOn:
			app.FocusPane = PaneDepBy
		}
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
			app.RefreshLixenFlat()
			app.Message = "filter cleared"
		}
		return false, false
	}

	switch app.FocusPane {
	case PaneLixen:
		return app.handleLixenPaneEvent(ev)
	case PaneTree:
		return app.handleTreePaneEvent(ev)
	case PaneDepBy:
		return app.handleDepByPaneEvent(ev)
	case PaneDepOn:
		return app.handleDepOnPaneEvent(ev)
	}
	return false, false
}

// handleLixenPaneEvent processes input when lixen pane focused
func (app *AppState) handleLixenPaneEvent(ev terminal.Event) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveLixenCursor(1)
		case 'k':
			app.moveLixenCursor(-1)
		case 'h':
			app.collapseLixenItem()
		case 'l':
			app.expandLixenItem()
		case ' ':
			app.toggleLixenSelection()
		case 'a':
			app.selectAllVisibleLixenTags()
		case '0':
			app.jumpLixenToStart()
		case '$':
			app.jumpLixenToEnd()
		case 'H':
			app.collapseAllLixenItems()
		case 'L':
			app.expandAllLixenItems()
		}

	case terminal.KeyUp:
		app.moveLixenCursor(-1)
	case terminal.KeyDown:
		app.moveLixenCursor(1)
	case terminal.KeyLeft:
		app.collapseLixenItem()
	case terminal.KeyRight:
		app.expandLixenItem()
	case terminal.KeySpace:
		app.toggleLixenSelection()
	case terminal.KeyPageUp:
		app.pageLixenCursor(-1)
	case terminal.KeyPageDown:
		app.pageLixenCursor(1)
	case terminal.KeyHome:
		app.jumpLixenToStart()
	case terminal.KeyEnd:
		app.jumpLixenToEnd()
	}

	return false, false
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

// handleDepByPaneEvent processes input when depended-by pane focused
func (app *AppState) handleDepByPaneEvent(ev terminal.Event) (quit, output bool) {
	return app.handleDetailPaneEvent(ev, app.DepByState)
}

// handleDepOnPaneEvent processes input when depends-on pane focused
func (app *AppState) handleDepOnPaneEvent(ev terminal.Event) (quit, output bool) {
	return app.handleDetailPaneEvent(ev, app.DepOnState)
}

// navigateToDepByPackage navigates to first file in depended-by package
func (app *AppState) navigateToDepByPackage() {
	pkgDir := app.getCurrentFilePackageDir()
	if pkgDir == "" {
		return
	}

	depByPkgs := app.Index.ReverseDeps[pkgDir]
	if len(depByPkgs) == 0 {
		return
	}

	// Navigate to first package in list
	targetPkg := depByPkgs[0]
	app.navigateTreeToPackage(targetPkg)
}

// handleDetailPaneEvent generic handler for both detail panes
func (app *AppState) handleDetailPaneEvent(ev terminal.Event, state *DetailPaneState) (quit, output bool) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			app.moveDetailCursor(state, 1)
		case 'k':
			app.moveDetailCursor(state, -1)
		case 'h':
			app.collapseDetailItem(state)
		case 'l':
			app.expandOrNavDetailItem(state)
		case 'H':
			state.Expanded = make(map[string]bool)
			app.refreshDetailPanes() // Recompute flat list
			state.Cursor = 0
			state.Scroll = 0
		case 'L':
			// Expand all visible headers
			for _, item := range state.FlatItems {
				if item.IsHeader {
					state.Expanded[item.Key] = true
				}
			}
			app.refreshDetailPanes()
		case '0':
			state.Cursor = 0
			state.Scroll = 0
		case '$':
			if len(state.FlatItems) > 0 {
				state.Cursor = len(state.FlatItems) - 1
				app.moveDetailCursor(state, 0)
			}
		}

	case terminal.KeyUp:
		app.moveDetailCursor(state, -1)
	case terminal.KeyDown:
		app.moveDetailCursor(state, 1)
	case terminal.KeyLeft:
		app.collapseDetailItem(state)
	case terminal.KeyRight:
		app.expandOrNavDetailItem(state)
	case terminal.KeyPageUp:
		app.pageDetailCursor(state, -1)
	case terminal.KeyPageDown:
		app.pageDetailCursor(state, 1)
	case terminal.KeyHome:
		state.Cursor = 0
		state.Scroll = 0
	case terminal.KeyEnd:
		if len(state.FlatItems) > 0 {
			state.Cursor = len(state.FlatItems) - 1
			app.moveDetailCursor(state, 0)
		}
	case terminal.KeyEnter:
		app.expandOrNavDetailItem(state)
	}
	return false, false
}

func (app *AppState) moveDetailCursor(state *DetailPaneState, delta int) {
	if len(state.FlatItems) == 0 {
		return
	}
	state.Cursor += delta
	if state.Cursor < 0 {
		state.Cursor = 0
	}
	if state.Cursor >= len(state.FlatItems) {
		state.Cursor = len(state.FlatItems) - 1
	}

	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if state.Cursor < state.Scroll {
		state.Scroll = state.Cursor
	}
	if state.Cursor >= state.Scroll+visibleRows {
		state.Scroll = state.Cursor - visibleRows + 1
	}
}

func (app *AppState) pageDetailCursor(state *DetailPaneState, direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveDetailCursor(state, delta)
}

func (app *AppState) collapseDetailItem(state *DetailPaneState) {
	if len(state.FlatItems) == 0 {
		return
	}
	item := state.FlatItems[state.Cursor]
	if item.IsHeader && item.Expanded {
		state.Expanded[item.Key] = false
		app.refreshDetailPanes()
	} else if item.Level > 0 {
		// Jump to parent
		for i := state.Cursor - 1; i >= 0; i-- {
			if state.FlatItems[i].IsHeader && state.FlatItems[i].Level < item.Level {
				// Naive parent finding based on level/header, sufficient for 2-level
				if strings.HasPrefix(item.Key, state.FlatItems[i].Key) { // Key check is safer
					state.Cursor = i
					app.moveDetailCursor(state, 0)
					break
				}
			}
		}
	}
}

func (app *AppState) expandOrNavDetailItem(state *DetailPaneState) {
	if len(state.FlatItems) == 0 {
		return
	}
	item := state.FlatItems[state.Cursor]

	if item.IsHeader {
		if !item.Expanded {
			state.Expanded[item.Key] = true
			app.refreshDetailPanes()
		}
	} else if item.IsFile && item.Path != "" {
		// Navigation (Pane 3)
		app.navigateTreeToFile(item.Path)
	}
}

// navigateTreeToFile expands tree and positions cursor on file
func (app *AppState) navigateTreeToFile(path string) {
	// Find file in tree
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
				app.TreeCursor = i
				app.moveTreeCursor(0)
				app.FocusPane = PaneTree
				app.Message = fmt.Sprintf("navigated to %s", path)
				app.triggerAnalysis() // Ensure analysis runs on nav
				app.refreshDetailPanes()
				return
			}
		}
	}
}

// triggerAnalysis runs AST analysis if cursor is on a file
func (app *AppState) triggerAnalysis() {
	if len(app.TreeFlat) == 0 {
		return
	}
	node := app.TreeFlat[app.TreeCursor]
	if node.IsDir {
		return
	}

	if _, ok := app.DepAnalysisCache[node.Path]; !ok {
		// Not in cache, analyze
		// Note: Synchronous for now, per plan. Can be made async if needed.
		analysis, err := AnalyzeFileDependencies(node.Path, app.Index.ModulePath)
		if err == nil {
			app.DepAnalysisCache[node.Path] = analysis
		}
	}
}

// refreshDetailPanes rebuilds flat lists for both detail panes based on current tree selection
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

	// Get raw reverse deps: importedPkg -> importingFiles
	files := app.Index.ReverseDeps[pkgDir]
	if len(files) == 0 {
		return
	}

	// Prepare target definitions for intersection if a file is selected
	var targetDefs map[string]bool
	targetFile := app.getCurrentFileInfo()
	// Only calculate usage if a specific file is selected (not just dir)
	if targetFile != nil && len(targetFile.Definitions) > 0 {
		targetDefs = make(map[string]bool, len(targetFile.Definitions))
		for _, def := range targetFile.Definitions {
			targetDefs[def] = true
		}
	}

	// Resolve full import path for the currently selected package
	fullImportPath := app.Index.ModulePath
	if pkgDir != "." {
		fullImportPath += "/" + pkgDir
	}

	// Structure to hold temp data for sorting
	type depFile struct {
		Path     string
		HasUsage bool
	}

	// Group files by their package
	filesByPkg := make(map[string][]*depFile)

	for _, fPath := range files {
		hasUsage := false

		// If we have a target file with definitions, check for usage
		if targetDefs != nil {
			// Check cache or analyze
			analysis, ok := app.DepAnalysisCache[fPath]
			if !ok {
				// On-demand analysis
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

	for _, p := range pkgs {
		pkgFiles := filesByPkg[p]

		// Sort files: Usage first, then alphabetical
		sort.Slice(pkgFiles, func(i, j int) bool {
			if pkgFiles[i].HasUsage && !pkgFiles[j].HasUsage {
				return true
			}
			if !pkgFiles[i].HasUsage && pkgFiles[j].HasUsage {
				return false
			}
			return pkgFiles[i].Path < pkgFiles[j].Path
		})

		// Default to expanded if new
		if _, known := app.DepByState.Expanded[p]; !known {
			app.DepByState.Expanded[p] = true
		}
		isExpanded := app.DepByState.Expanded[p]

		// Format label for root package
		label := p
		if label == "." {
			label = "(root)"
		}

		// Add Package Header
		app.DepByState.FlatItems = append(app.DepByState.FlatItems, DetailItem{
			Level:    0,
			Label:    label,
			Key:      p,
			IsHeader: true,
			Expanded: isExpanded,
			IsLocal:  true,
		})

		if isExpanded {
			for _, f := range pkgFiles {
				app.DepByState.FlatItems = append(app.DepByState.FlatItems, DetailItem{
					Level:    1,
					Label:    filepath.Base(f.Path),
					Key:      f.Path,
					IsFile:   true,
					Path:     f.Path,
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

	// Analysis result (symbols)
	analysis := app.DepAnalysisCache[fi.Path]

	importPaths := make([]string, 0)
	if analysis != nil {
		for p := range analysis.UsedSymbols {
			importPaths = append(importPaths, p)
		}
	} else {
		// Fallback: we don't have full paths in FileInfo easily mapping to short names
		// without scanning Index packages. Waiting for analysis trigger.
		return
	}
	sort.Strings(importPaths)

	for _, path := range importPaths {
		isLocal := strings.HasPrefix(path, app.Index.ModulePath)

		// Default to expanded if new
		if _, known := app.DepOnState.Expanded[path]; !known {
			app.DepOnState.Expanded[path] = true
		}
		isExpanded := app.DepOnState.Expanded[path]

		// Display name: strip module path for locals for brevity?
		// User req: "green for internal package names"
		dispName := path
		if isLocal {
			dispName = strings.TrimPrefix(path, app.Index.ModulePath+"/")
		}

		symbols := analysis.UsedSymbols[path]
		hasSymbols := len(symbols) > 0 && isLocal // Only show symbols for local

		app.DepOnState.FlatItems = append(app.DepOnState.FlatItems, DetailItem{
			Level:    0,
			Label:    dispName,
			Key:      path,
			IsHeader: hasSymbols, // Only expandable if we have symbols to show
			Expanded: isExpanded,
			IsLocal:  isLocal,
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

// navigateToDepOnPackage navigates to first file in depends-on package
func (app *AppState) navigateToDepOnPackage() {
	fi := app.getCurrentFileInfo()
	if fi == nil || len(fi.Imports) == 0 {
		return
	}

	// Find package directory for first import
	targetName := fi.Imports[0]
	for pkgDir, pkg := range app.Index.Packages {
		if pkg.Name == targetName {
			app.navigateTreeToPackage(pkgDir)
			return
		}
	}
}

// navigateTreeToPackage expands tree and positions cursor on package
func (app *AppState) navigateTreeToPackage(pkgDir string) {
	// Find package in tree and expand path to it
	var findAndExpand func(node *TreeNode) bool
	findAndExpand = func(node *TreeNode) bool {
		if node.Path == pkgDir {
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

		// Position cursor on the package
		for i, n := range app.TreeFlat {
			if n.Path == pkgDir {
				app.TreeCursor = i
				app.moveTreeCursor(0) // Adjust scroll
				app.FocusPane = PaneTree
				app.Message = fmt.Sprintf("navigated to %s", pkgDir)
				return
			}
		}
	}
}

// Lixen pane navigation

func (app *AppState) moveLixenCursor(delta int) {
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	ui.Cursor += delta
	if ui.Cursor < 0 {
		ui.Cursor = 0
	}
	if ui.Cursor >= len(ui.Flat) {
		ui.Cursor = len(ui.Flat) - 1
	}

	// Adjust scroll
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}

	if ui.Cursor < ui.Scroll {
		ui.Scroll = ui.Cursor
	}
	if ui.Cursor >= ui.Scroll+visibleRows {
		ui.Scroll = ui.Cursor - visibleRows + 1
	}
}

func (app *AppState) pageLixenCursor(direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveLixenCursor(delta)
}

func (app *AppState) jumpLixenToStart() {
	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}
	ui.Cursor = 0
	ui.Scroll = 0
}

func (app *AppState) jumpLixenToEnd() {
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}
	ui.Cursor = len(ui.Flat) - 1
	app.moveLixenCursor(0)
}

func (app *AppState) collapseLixenItem() {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	item := ui.Flat[ui.Cursor]

	switch item.Type {
	case TagItemTypeGroup:
		if ui.GroupExpanded[item.Group] {
			ui.GroupExpanded[item.Group] = false
			app.RefreshLixenFlat()
		}
	case TagItemTypeModule:
		moduleKey := item.Group + "." + item.Module
		if ui.ModuleExpanded[moduleKey] {
			ui.ModuleExpanded[moduleKey] = false
			app.RefreshLixenFlat()
		} else {
			// Navigate to parent group
			for i := ui.Cursor - 1; i >= 0; i-- {
				if ui.Flat[i].Type == TagItemTypeGroup && ui.Flat[i].Group == item.Group {
					ui.Cursor = i
					app.moveLixenCursor(0)
					break
				}
			}
		}
	case TagItemTypeTag:
		// Navigate to parent (module or group)
		for i := ui.Cursor - 1; i >= 0; i-- {
			if item.Module != DirectTagsModule {
				if ui.Flat[i].Type == TagItemTypeModule && ui.Flat[i].Group == item.Group && ui.Flat[i].Module == item.Module {
					ui.Cursor = i
					app.moveLixenCursor(0)
					return
				}
			}
			if ui.Flat[i].Type == TagItemTypeGroup && ui.Flat[i].Group == item.Group {
				ui.Cursor = i
				app.moveLixenCursor(0)
				return
			}
		}
	}
	_ = cat // Used for context
}

func (app *AppState) expandLixenItem() {
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	item := ui.Flat[ui.Cursor]

	switch item.Type {
	case TagItemTypeGroup:
		if !ui.GroupExpanded[item.Group] {
			ui.GroupExpanded[item.Group] = true
			app.RefreshLixenFlat()
		}
	case TagItemTypeModule:
		moduleKey := item.Group + "." + item.Module
		if !ui.ModuleExpanded[moduleKey] {
			ui.ModuleExpanded[moduleKey] = true
			app.RefreshLixenFlat()
		}
	}
}

func (app *AppState) collapseAllLixenItems() {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	catIdx := app.Index.Category(cat)
	if ui == nil || catIdx == nil {
		return
	}

	for _, group := range catIdx.Groups {
		ui.GroupExpanded[group] = false
	}
	for group, modules := range catIdx.Modules {
		for _, mod := range modules {
			ui.ModuleExpanded[group+"."+mod] = false
		}
	}

	app.RefreshLixenFlat()
	ui.Cursor = 0
	ui.Scroll = 0
	app.Message = "collapsed all groups"
}

func (app *AppState) expandAllLixenItems() {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	catIdx := app.Index.Category(cat)
	if ui == nil || catIdx == nil {
		return
	}

	for _, group := range catIdx.Groups {
		ui.GroupExpanded[group] = true
	}
	for group, modules := range catIdx.Modules {
		for _, mod := range modules {
			ui.ModuleExpanded[group+"."+mod] = true
		}
	}

	app.RefreshLixenFlat()
	app.moveLixenCursor(0)
	app.Message = "expanded all groups"
}

func (app *AppState) toggleLixenSelection() {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	if ui == nil || len(ui.Flat) == 0 {
		return
	}

	item := ui.Flat[ui.Cursor]

	switch item.Type {
	case TagItemTypeGroup:
		if app.allFilesWithGroupSelected(cat, item.Group) {
			count := app.deselectFilesWithGroup(cat, item.Group)
			app.Message = fmt.Sprintf("deselected %d files from #%s", count, item.Group)
		} else {
			count := app.selectFilesWithGroup(cat, item.Group)
			app.Message = fmt.Sprintf("selected %d files with #%s", count, item.Group)
		}
	case TagItemTypeModule:
		if app.allFilesWithModuleSelected(cat, item.Group, item.Module) {
			count := app.deselectFilesWithModule(cat, item.Group, item.Module)
			app.Message = fmt.Sprintf("deselected %d files from %s", count, item.Module)
		} else {
			count := app.selectFilesWithModule(cat, item.Group, item.Module)
			app.Message = fmt.Sprintf("selected %d files with %s", count, item.Module)
		}
	case TagItemTypeTag:
		if app.allFilesWithTagSelected(cat, item.Group, item.Module, item.Tag) {
			count := app.deselectFilesWithTag(cat, item.Group, item.Module, item.Tag)
			app.Message = fmt.Sprintf("deselected %d files from %s", count, item.Tag)
		} else {
			count := app.selectFilesWithTag(cat, item.Group, item.Module, item.Tag)
			app.Message = fmt.Sprintf("selected %d files with %s", count, item.Tag)
		}
	}
}

func (app *AppState) selectAllVisibleLixenTags() {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}

	count := 0
	for _, item := range ui.Flat {
		if item.Type == TagItemTypeTag {
			count += app.selectFilesWithTag(cat, item.Group, item.Module, item.Tag)
		}
	}
	app.Message = fmt.Sprintf("selected %d files from visible tags", count)
}

// Tree pane navigation

func (app *AppState) jumpTreeToStart() {
	if len(app.TreeFlat) == 0 {
		return
	}
	app.TreeCursor = 0
	app.TreeScroll = 0
}

func (app *AppState) jumpTreeToEnd() {
	if len(app.TreeFlat) == 0 {
		return
	}
	app.TreeCursor = len(app.TreeFlat) - 1
	app.moveTreeCursor(0)
}

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

func (app *AppState) expandAllDirs() {
	expandAllRecursive(app.TreeRoot)
	app.RefreshTreeFlat()
	app.moveTreeCursor(0)
	app.Message = "expanded all directories"
}

func (app *AppState) pageTreeCursor(direction int) {
	visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
	if visibleRows < 1 {
		visibleRows = 1
	}
	delta := (visibleRows / 2) * direction
	if delta == 0 {
		delta = direction
	}
	app.moveTreeCursor(delta)
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

func (app *AppState) moveTreeCursor(delta int) {
	if len(app.TreeFlat) == 0 {
		return
	}

	// Check if cursor moved
	prevCursor := app.TreeCursor

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

	if app.TreeCursor != prevCursor || delta == 0 { // delta 0 implies refresh
		app.triggerAnalysis()
		app.refreshDetailPanes()
	}
}

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
				app.moveTreeCursor(0)
				break
			}
		}
	}
}

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

func (app *AppState) toggleTreeSelection() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]

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

func (app *AppState) selectAllVisible() {
	for _, node := range app.TreeFlat {
		if !node.IsDir {
			app.Selected[node.Path] = true
		}
	}
	app.Message = "selected all visible files"
}

func (app *AppState) EnterPreview() {
	app.PreviewFiles = app.ComputeOutputFiles()
	app.PreviewMode = true
	app.PreviewScroll = 0
}

func (app *AppState) copyOutputToClipboard() {
	files := app.ComputeOutputFiles()
	if len(files) == 0 {
		app.Message = "no files to copy"
		return
	}

	cmd := exec.Command("wl-copy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}

	if err := cmd.Start(); err != nil {
		return
	}

	for _, f := range files {
		fmt.Fprintf(stdin, "./%s\n", f)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return
	}

	app.Message = fmt.Sprintf("copied %d files to clipboard", len(files))
}

func (app *AppState) applyCurrentPaneFilter() {
	switch app.FocusPane {
	case PaneLixen:
		app.applyLixenPaneFilter()
	case PaneTree:
		app.applyTreePaneFilter()
	case PaneDepBy, PaneDepOn:
		app.Message = "filter not available in dep panes"
	}
}

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

func (app *AppState) getCurrentCategoryUI() *CategoryUIState {
	if app.CurrentCategory == "" {
		return nil
	}
	ui := app.CategoryUI[app.CurrentCategory]
	if ui == nil {
		ui = NewCategoryUIState()
		app.CategoryUI[app.CurrentCategory] = ui
	}
	return ui
}

func (app *AppState) switchCategory(delta int) {
	if len(app.CategoryNames) <= 1 {
		return
	}

	currentIdx := -1
	for i, cat := range app.CategoryNames {
		if cat == app.CurrentCategory {
			currentIdx = i
			break
		}
	}

	if currentIdx == -1 {
		currentIdx = 0
	}

	newIdx := (currentIdx + delta + len(app.CategoryNames)) % len(app.CategoryNames)
	newCat := app.CategoryNames[newIdx]

	if newCat == app.CurrentCategory {
		return
	}

	var currentFile string
	if len(app.TreeFlat) > 0 && app.TreeCursor < len(app.TreeFlat) {
		node := app.TreeFlat[app.TreeCursor]
		if !node.IsDir {
			currentFile = node.Path
		}
	}

	app.CurrentCategory = newCat

	if app.CategoryUI[newCat] == nil {
		app.CategoryUI[newCat] = NewCategoryUIState()
	}

	app.RefreshLixenFlat()

	if currentFile != "" {
		app.positionLixenCursorForFile(currentFile)
	}

	app.Message = fmt.Sprintf("category: %s", newCat)
}

func (app *AppState) positionLixenCursorForFile(path string) {
	fi := app.Index.Files[path]
	if fi == nil {
		return
	}

	catTags := fi.CategoryTags(app.CurrentCategory)
	if catTags == nil || len(catTags) == 0 {
		ui := app.getCurrentCategoryUI()
		if ui != nil {
			ui.Cursor = 0
			ui.Scroll = 0
		}
		return
	}

	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}

	for group := range catTags {
		for i, item := range ui.Flat {
			if item.Type == TagItemTypeGroup && item.Group == group {
				ui.Cursor = i
				visibleRows := app.Height - headerHeight - statusHeight - helpHeight - 2
				if visibleRows < 1 {
					visibleRows = 1
				}
				if ui.Cursor < ui.Scroll {
					ui.Scroll = ui.Cursor
				}
				if ui.Cursor >= ui.Scroll+visibleRows {
					ui.Scroll = ui.Cursor - visibleRows + 1
				}
				return
			}
		}
	}
}

func (app *AppState) RefreshLixenFlat() {
	cat := app.CurrentCategory
	if cat == "" {
		return
	}

	catIdx := app.Index.Category(cat)
	if catIdx == nil {
		return
	}

	ui := app.getCurrentCategoryUI()
	if ui == nil {
		return
	}

	ui.Flat = nil

	for _, group := range catIdx.Groups {
		groupExpanded := true
		if exp, ok := ui.GroupExpanded[group]; ok {
			groupExpanded = exp
		} else {
			ui.GroupExpanded[group] = true
		}

		ui.Flat = append(ui.Flat, TagItem{
			Type:     TagItemTypeGroup,
			Group:    group,
			Expanded: groupExpanded,
		})

		if !groupExpanded {
			continue
		}

		if tags, ok := catIdx.Tags[group][DirectTagsModule]; ok {
			for _, tag := range tags {
				ui.Flat = append(ui.Flat, TagItem{
					Type:   TagItemTypeTag,
					Group:  group,
					Module: DirectTagsModule,
					Tag:    tag,
				})
			}
		}

		if modules, ok := catIdx.Modules[group]; ok {
			for _, module := range modules {
				moduleKey := group + "." + module
				moduleExpanded := true
				if exp, ok := ui.ModuleExpanded[moduleKey]; ok {
					moduleExpanded = exp
				} else {
					ui.ModuleExpanded[moduleKey] = true
				}

				ui.Flat = append(ui.Flat, TagItem{
					Type:     TagItemTypeModule,
					Group:    group,
					Module:   module,
					Expanded: moduleExpanded,
				})

				if !moduleExpanded {
					continue
				}

				if tags, ok := catIdx.Tags[group][module]; ok {
					for _, tag := range tags {
						ui.Flat = append(ui.Flat, TagItem{
							Type:   TagItemTypeTag,
							Group:  group,
							Module: module,
							Tag:    tag,
						})
					}
				}
			}
		}
	}

	if ui.Cursor >= len(ui.Flat) {
		ui.Cursor = len(ui.Flat) - 1
	}
	if ui.Cursor < 0 {
		ui.Cursor = 0
	}
}