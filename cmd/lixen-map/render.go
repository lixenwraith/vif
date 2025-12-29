package main

import (
	"fmt"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// Render draws the complete UI to the terminal
func (app *AppState) Render() {
	w, h := app.Width, app.Height
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: app.Theme.Fg, Bg: app.Theme.Bg}
	}

	root := tui.NewRegion(cells, w, 0, 0, w, h)

	// Render overlays if visible, otherwise main view
	if app.Editor != nil && app.Editor.Visible {
		app.renderEditor(root)
	} else if app.Viewer != nil && app.Viewer.Visible {
		app.renderFileViewer(root)
	} else {
		app.renderMain(root)
	}

	app.Term.Flush(cells, w, h)
}

// renderMain draws the main layout: header, content panes, status bar
func (app *AppState) renderMain(r tui.Region) {
	// Layout: header (1 row) | content | status (2 rows)
	header, rest := tui.SplitVFixed(r, 1)
	content, status := tui.SplitVFixed(rest, rest.H-2)

	app.renderHeader(header)
	app.renderPanes(content)
	app.renderStatus(status)
}

// renderHeader draws the top header bar with title and stats
func (app *AppState) renderHeader(r tui.Region) {
	r.Fill(app.Theme.HeaderBg)

	// Title on the left
	r.Text(1, 0, "LIXEN-MAP", app.Theme.HeaderFg, app.Theme.HeaderBg, terminal.AttrBold)

	// Stats on the right using StatusBar
	totalFiles, depFiles, totalSize, depSize := app.computeOutputStats()

	sections := []tui.BarSection{
		{
			Label:      "Deps: ",
			Value:      app.formatDepsValue(depFiles),
			LabelStyle: tui.Style{Fg: app.Theme.StatusFg},
			ValueStyle: tui.Style{Fg: app.Theme.HeaderFg},
		},
		{
			Label:      "Output: ",
			Value:      fmt.Sprintf("%d files", totalFiles),
			LabelStyle: tui.Style{Fg: app.Theme.StatusFg},
			ValueStyle: tui.Style{Fg: app.Theme.HeaderFg},
		},
		{
			Label:      "Size: ",
			Value:      app.formatSizeValue(totalSize, depSize),
			LabelStyle: tui.Style{Fg: app.Theme.StatusFg},
			ValueStyle: tui.Style{Fg: app.sizeColor(totalSize)},
		},
	}

	r.StatusBar(0, sections, tui.BarOpts{
		Separator: " | ",
		SepStyle:  tui.Style{Fg: app.Theme.Border},
		Bg:        app.Theme.HeaderBg,
		Align:     tui.BarAlignRight,
		Padding:   1,
	})
}

// formatDepsValue formats the dependency expansion status for header
func (app *AppState) formatDepsValue(depFiles int) string {
	if app.ExpandDeps {
		return fmt.Sprintf("%d (+%d)", depFiles, app.DepthLimit)
	}
	return "OFF"
}

// formatSizeValue formats total size with optional dependency size
func (app *AppState) formatSizeValue(totalSize, depSize int64) string {
	s := formatSize(totalSize)
	if app.ExpandDeps && depSize > 0 {
		s += fmt.Sprintf(" (+%s dep)", formatSize(depSize))
	}
	return s
}

// sizeColor returns warning color if size exceeds threshold
func (app *AppState) sizeColor(size int64) terminal.RGB {
	if size > SizeWarningThreshold {
		return app.Theme.Warning
	}
	return app.Theme.HeaderFg
}

// renderPanes draws the 4-pane layout with dividers
func (app *AppState) renderPanes(r tui.Region) {
	panes := tui.SplitHEqual(r, 4, 1)

	for _, x := range tui.DividerPositions(r.W, 4, 1) {
		r.VLine(x, tui.LineSingle, app.Theme.Border)
	}

	defs := []struct {
		pane  Pane
		title string
		draw  func(tui.Region)
	}{
		{PaneLixen, app.lixenTitle(), app.renderLixenPane},
		{PaneTree, "PACKAGES / FILES", app.renderTreePane},
		{PaneDepBy, "DEPENDED BY", app.renderDepByPane},
		{PaneDepOn, "DEPENDS ON", app.renderDepOnPane},
	}

	for i, d := range defs {
		content := panes[i].TitledPaneFocused(
			d.title,
			app.Theme.StatusFg,
			app.Theme.Bg,
			app.Theme.FocusBg,
			app.FocusPane == d.pane,
		)
		d.draw(content)
	}
}

// lixenTitle returns the title for the lixen pane including current category
func (app *AppState) lixenTitle() string {
	if app.CurrentCategory == "" {
		return "LIXEN"
	}
	return fmt.Sprintf("LIXEN: %s", app.CurrentCategory)
}

// renderLixenPane draws the category/tag hierarchy pane
func (app *AppState) renderLixenPane(r tui.Region) {
	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	if ui == nil || cat == "" {
		r.Fill(app.Theme.Bg)
		r.TextCenter(r.H/2, "(no categories)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	// Update visible height for scroll calculations
	ui.TreeState.SetVisible(r.H)

	// Build and cache tree nodes from flat tag items
	ui.Nodes = app.buildLixenNodes(cat, ui)
	if len(ui.Nodes) == 0 {
		r.Text(1, 0, "(no tags)", app.Theme.Unselected, terminal.RGB{}, terminal.AttrNone)
		return
	}

	// Determine background based on focus
	bg := app.Theme.Bg
	if app.FocusPane == PaneLixen {
		bg = app.Theme.FocusBg
	}

	r.Tree(ui.Nodes, ui.TreeState.Cursor, ui.TreeState.Scroll, tui.TreeOpts{
		CursorBg:    app.Theme.CursorBg,
		DefaultBg:   bg,
		IndentWidth: 2,
		IconWidth:   2,
	})
}

// buildLixenNodes converts flat TagItems to tui.TreeNodes for rendering
func (app *AppState) buildLixenNodes(cat string, ui *CategoryUIState) []tui.TreeNode {
	hasFilter := app.Filter.HasActiveFilter()
	nodes := make([]tui.TreeNode, 0, len(ui.Flat))

	for _, item := range ui.Flat {
		var node tui.TreeNode

		switch item.Type {
		case TagItemTypeGroup:
			isFiltered := app.isGroupFiltered(cat, item.Group)
			dimmed := hasFilter && !isFiltered
			selState := app.computeGroupSelectionState(cat, item.Group)
			count := app.countFilesInGroup(cat, item.Group)
			key := "g:" + item.Group

			node = tui.TreeNode{
				Key:         key,
				Label:       "#" + item.Group,
				Expandable:  true,
				Expanded:    ui.Expansion.IsExpanded(key),
				Depth:       0,
				Check:       app.toCheckState(selState),
				CheckFg:     app.checkColor(selState, dimmed),
				Style:       app.groupStyle(dimmed),
				Suffix:      fmt.Sprintf(" (%d)", count),
				SuffixStyle: tui.Style{Fg: app.suffixColor(dimmed)},
				Data:        LixenNodeData{Type: TagItemTypeGroup, Category: cat, Group: item.Group},
			}

		case TagItemTypeModule:
			isFiltered := app.isModuleFiltered(cat, item.Group, item.Module)
			dimmed := hasFilter && !isFiltered
			selState := app.computeModuleSelectionState(cat, item.Group, item.Module)
			count := app.countFilesInModule(cat, item.Group, item.Module)
			key := fmt.Sprintf("m:%s.%s", item.Group, item.Module)

			node = tui.TreeNode{
				Key:         key,
				Label:       item.Module,
				Expandable:  true,
				Expanded:    ui.Expansion.IsExpanded(key),
				Depth:       1,
				Check:       app.toCheckState(selState),
				CheckFg:     app.checkColor(selState, dimmed),
				Style:       app.moduleStyle(dimmed),
				Suffix:      fmt.Sprintf(" (%d)", count),
				SuffixStyle: tui.Style{Fg: app.suffixColor(dimmed)},
				Data:        LixenNodeData{Type: TagItemTypeModule, Category: cat, Group: item.Group, Module: item.Module},
			}

		case TagItemTypeTag:
			isFiltered := app.isTagFiltered(cat, item.Group, item.Module, item.Tag)
			dimmed := hasFilter && !isFiltered
			selState := app.computeTagSelectionState(cat, item.Group, item.Module, item.Tag)
			count := app.countFilesWithTag(cat, item.Group, item.Module, item.Tag)

			// Depth: 0 for category-direct (2-level), 1 for group-direct (3-level), 2 for module tags (4-level)
			depth := 2
			if item.Group == DirectTagsGroup && item.Module == DirectTagsModule {
				depth = 0
			} else if item.Module == DirectTagsModule {
				depth = 1
			}

			node = tui.TreeNode{
				Key:         fmt.Sprintf("t:%s.%s.%s", item.Group, item.Module, item.Tag),
				Label:       item.Tag,
				Depth:       depth,
				Check:       app.toCheckState(selState),
				CheckFg:     app.checkColor(selState, dimmed),
				Style:       app.tagStyle(dimmed),
				Suffix:      fmt.Sprintf(" (%d)", count),
				SuffixStyle: tui.Style{Fg: app.suffixColor(dimmed)},
				Data:        LixenNodeData{Type: TagItemTypeTag, Category: cat, Group: item.Group, Module: item.Module, Tag: item.Tag},
			}
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// renderTreePane draws the file/directory tree pane
func (app *AppState) renderTreePane(r tui.Region) {
	if len(app.TreeFlat) == 0 {
		r.TextCenter(r.H/2, "(no files)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	app.TreeState.SetVisible(r.H)
	nodes := app.buildTreeNodes()

	bg := app.Theme.Bg
	if app.FocusPane == PaneTree {
		bg = app.Theme.FocusBg
	}

	r.Tree(nodes, app.TreeState.Cursor, app.TreeState.Scroll, tui.TreeOpts{
		CursorBg:    app.Theme.CursorBg,
		DefaultBg:   bg,
		IndentWidth: 2,
		IconWidth:   2,
	})
}

// buildTreeNodes converts flat TreeNodes to tui.TreeNodes for rendering
func (app *AppState) buildTreeNodes() []tui.TreeNode {
	hasFilter := app.Filter.HasActiveFilter()

	// Compute dependency-expanded files for visual indication
	depExpanded := make(map[string]bool)
	if app.ExpandDeps && len(app.Selected) > 0 {
		depExpanded = app.computeDepExpandedFiles()
	}

	nodes := make([]tui.TreeNode, 0, len(app.TreeFlat))

	for _, tn := range app.TreeFlat {
		matchesFilter := app.nodeMatchesFilter(tn)
		dimmed := hasFilter && !matchesFilter

		var node tui.TreeNode

		if tn.IsDir {
			selCount, totalCount := app.countDirSelection(tn)

			// Build suffix showing selection ratio
			var suffix string
			if totalCount > 0 {
				suffix = fmt.Sprintf(" [%d/%d]", selCount, totalCount)
			}

			node = tui.TreeNode{
				Key:         "d:" + tn.Path,
				Label:       tn.Name,
				Expandable:  true,
				Expanded:    tn.Expanded,
				Depth:       tn.Depth,
				Check:       app.dirCheckState(selCount, totalCount),
				CheckFg:     app.dirCheckColor(selCount, totalCount, dimmed),
				Style:       app.dirStyle(dimmed),
				Suffix:      suffix,
				SuffixStyle: tui.Style{Fg: app.suffixColor(dimmed)},
			}
		} else {
			// File node
			isSelected := app.Selected[tn.Path]
			isDepExpanded := depExpanded[tn.Path]

			// Determine checkbox state
			check := tui.CheckNone
			checkFg := app.Theme.Unselected
			if isSelected {
				check = tui.CheckFull
				checkFg = app.Theme.Selected
			} else if isDepExpanded {
				check = tui.CheckPlus
				checkFg = app.Theme.Expanded
			}
			if dimmed {
				checkFg = app.Theme.Unselected
			}

			// Determine text color
			fg := app.Theme.FileFg
			if tn.FileInfo != nil && tn.FileInfo.IsAll {
				fg = app.Theme.AllTagFg
			}
			if dimmed {
				fg = app.Theme.Unselected
			}

			// Build suffix with group summary
			var suffix string
			if tn.FileInfo != nil {
				suffix = getFileGroupSummary(tn.FileInfo, app.CurrentCategory)
				if suffix != "" {
					suffix = " " + suffix
				}
			}

			node = tui.TreeNode{
				Key:         "f:" + tn.Path,
				Label:       tn.Name,
				Depth:       tn.Depth,
				Check:       check,
				CheckFg:     checkFg,
				Style:       tui.Style{Fg: fg},
				Suffix:      suffix,
				SuffixStyle: tui.Style{Fg: app.Theme.HintFg, Attr: terminal.AttrDim},
			}
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// renderDepByPane draws the "Depended By" pane showing reverse dependencies
func (app *AppState) renderDepByPane(r tui.Region) {
	pkgDir := app.getCurrentFilePackageDir()
	if pkgDir == "" {
		r.TextCenter(r.H/2, "(select a file)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	state := app.DepByState
	if len(state.FlatItems) == 0 {
		r.TextCenter(r.H/2, "(no dependents)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	state.TreeState.SetVisible(r.H)
	nodes := app.buildDetailNodes(state)

	bg := app.Theme.Bg
	if app.FocusPane == PaneDepBy {
		bg = app.Theme.FocusBg
	}

	r.Tree(nodes, state.TreeState.Cursor, state.TreeState.Scroll, tui.TreeOpts{
		CursorBg:    app.Theme.CursorBg,
		DefaultBg:   bg,
		IndentWidth: 2,
		IconWidth:   2,
	})
}

// renderDepOnPane draws the "Depends On" pane showing forward dependencies
func (app *AppState) renderDepOnPane(r tui.Region) {
	fi := app.getCurrentFileInfo()
	if fi == nil {
		r.TextCenter(r.H/2, "(select a file)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	state := app.DepOnState
	if len(state.FlatItems) == 0 {
		r.TextCenter(r.H/2, "(no dependencies)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	state.TreeState.SetVisible(r.H)
	nodes := app.buildDetailNodes(state)

	bg := app.Theme.Bg
	if app.FocusPane == PaneDepOn {
		bg = app.Theme.FocusBg
	}

	r.Tree(nodes, state.TreeState.Cursor, state.TreeState.Scroll, tui.TreeOpts{
		CursorBg:    app.Theme.CursorBg,
		DefaultBg:   bg,
		IndentWidth: 2,
		IconWidth:   2,
	})
}

// buildDetailNodes converts DetailItems to tui.TreeNodes for dependency panes
func (app *AppState) buildDetailNodes(state *DetailPaneState) []tui.TreeNode {
	nodes := make([]tui.TreeNode, 0, len(state.FlatItems))

	for _, item := range state.FlatItems {
		var node tui.TreeNode

		if item.IsHeader {
			// Package header row
			fg := app.Theme.ModuleFg
			if !item.IsLocal {
				fg = app.Theme.ExternalFg
			}

			// Checkbox for local packages only
			check := tui.CheckNone
			checkFg := terminal.RGB{}
			if item.IsLocal && item.PkgDir != "" {
				check, checkFg = app.getHeaderSelectionState(item.PkgDir)
			}

			node = tui.TreeNode{
				Key:        "h:" + item.Key,
				Label:      item.Label,
				Expandable: true,
				Expanded:   state.Expansion.IsExpanded(item.Key),
				Depth:      item.Level,
				Check:      check,
				CheckFg:    checkFg,
				Style:      tui.Style{Fg: fg, Attr: terminal.AttrBold},
			}
		} else if item.IsSymbol {
			// Symbol row (used in DepOn pane)
			node = tui.TreeNode{
				Key:    "s:" + item.Key,
				Label:  item.Label,
				Icon:   '•',
				IconFg: app.Theme.SymbolFg,
				Depth:  item.Level,
				Style:  tui.Style{Fg: app.Theme.SymbolFg},
			}
		} else if item.IsFile {
			// File row
			check := tui.CheckNone
			checkFg := app.Theme.Unselected
			if app.Selected[item.Path] {
				check = tui.CheckFull
				checkFg = app.Theme.Selected
			}

			fg := app.Theme.FileFg
			attr := terminal.AttrNone
			if item.HasUsage {
				fg = app.Theme.HeaderFg
				attr = terminal.AttrBold
			}

			// Badge for files with active symbol usage
			var badge rune
			var badgeFg terminal.RGB
			if item.HasUsage {
				badge = '★'
				badgeFg = app.Theme.ModuleFg
			}

			node = tui.TreeNode{
				Key:     "f:" + item.Key,
				Label:   item.Label,
				Depth:   item.Level,
				Check:   check,
				CheckFg: checkFg,
				Style:   tui.Style{Fg: fg, Attr: attr},
				Badge:   badge,
				BadgeFg: badgeFg,
			}
		}

		nodes = append(nodes, node)
	}

	return nodes
}

// renderStatus draws the status bar at the bottom
func (app *AppState) renderStatus(r tui.Region) {
	r.Fill(app.Theme.Bg)

	if app.InputMode {
		if app.InputMode {
			// Use Input which renders directly with cursor
			r.Input(0, tui.InputOpts{
				Label:    "Filter: ",
				LabelFg:  app.Theme.StatusFg,
				Text:     app.InputField.Value(),
				Cursor:   app.InputField.Cursor,
				CursorBg: app.Theme.HeaderFg,
				TextFg:   app.Theme.HeaderFg,
				Bg:       app.Theme.InputBg,
			})
		}
	} else if app.Filter.HasActiveFilter() {
		// Show active filter info
		modeStr := "OR"
		switch app.Filter.Mode {
		case FilterAND:
			modeStr = "AND"
		case FilterNOT:
			modeStr = "NOT"
		case FilterXOR:
			modeStr = "XOR"
		}
		filterStr := fmt.Sprintf("Filter: %d files [%s]", len(app.Filter.FilteredPaths), modeStr)
		r.Text(1, 0, filterStr, app.Theme.TagFg, app.Theme.Bg, terminal.AttrNone)
	}

	// Message or selection count on second row
	msg := app.Message
	if msg == "" {
		msg = fmt.Sprintf("Selected: %d files", len(app.Selected))
	}
	r.Text(1, 1, msg, app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
}

// --- Style helper methods ---

// toCheckState converts TagSelectionState to tui.CheckState
func (app *AppState) toCheckState(s TagSelectionState) tui.CheckState {
	switch s {
	case TagSelectFull:
		return tui.CheckFull
	case TagSelectPartial:
		return tui.CheckPartial
	default:
		return tui.CheckNone
	}
}

// checkColor returns appropriate checkbox color based on selection state
func (app *AppState) checkColor(s TagSelectionState, dimmed bool) terminal.RGB {
	if dimmed {
		return app.Theme.Unselected
	}
	switch s {
	case TagSelectFull:
		return app.Theme.Selected
	case TagSelectPartial:
		return app.Theme.Partial
	default:
		return app.Theme.Unselected
	}
}

// groupStyle returns style for tag group labels
func (app *AppState) groupStyle(dimmed bool) tui.Style {
	if dimmed {
		return tui.Style{Fg: app.Theme.Unselected, Attr: terminal.AttrBold}
	}
	return tui.Style{Fg: app.Theme.GroupFg, Attr: terminal.AttrBold}
}

// moduleStyle returns style for module labels
func (app *AppState) moduleStyle(dimmed bool) tui.Style {
	if dimmed {
		return tui.Style{Fg: app.Theme.Unselected}
	}
	return tui.Style{Fg: app.Theme.ModuleFg}
}

// tagStyle returns style for tag labels
func (app *AppState) tagStyle(dimmed bool) tui.Style {
	if dimmed {
		return tui.Style{Fg: app.Theme.Unselected}
	}
	return tui.Style{Fg: app.Theme.TagFg}
}

// dirStyle returns style for directory labels
func (app *AppState) dirStyle(dimmed bool) tui.Style {
	if dimmed {
		return tui.Style{Fg: app.Theme.Unselected}
	}
	return tui.Style{Fg: app.Theme.DirFg}
}

// suffixColor returns color for suffix text
func (app *AppState) suffixColor(dimmed bool) terminal.RGB {
	if dimmed {
		return app.Theme.Unselected
	}
	return app.Theme.StatusFg
}

// dirCheckState converts directory selection counts to checkbox state
func (app *AppState) dirCheckState(sel, total int) tui.CheckState {
	if total == 0 || sel == 0 {
		return tui.CheckNone
	}
	if sel == total {
		return tui.CheckFull
	}
	return tui.CheckPartial
}

// dirCheckColor returns checkbox color for directory based on selection
func (app *AppState) dirCheckColor(sel, total int, dimmed bool) terminal.RGB {
	if dimmed {
		return app.Theme.Unselected
	}
	if total == 0 || sel == 0 {
		return app.Theme.Unselected
	}
	if sel == total {
		return app.Theme.Selected
	}
	return app.Theme.Partial
}

// getHeaderSelectionState returns checkbox state and color for package headers
func (app *AppState) getHeaderSelectionState(pkgDir string) (tui.CheckState, terminal.RGB) {
	pkg := app.Index.Packages[pkgDir]
	if pkg == nil || len(pkg.Files) == 0 {
		return tui.CheckNone, app.Theme.Unselected
	}

	selected := 0
	for _, fi := range pkg.Files {
		if app.Selected[fi.Path] {
			selected++
		}
	}

	if selected == 0 {
		return tui.CheckNone, app.Theme.Unselected
	}
	if selected == len(pkg.Files) {
		return tui.CheckFull, app.Theme.Selected
	}
	return tui.CheckPartial, app.Theme.Partial
}

// --- Utility functions ---

// formatSize formats byte count as human-readable string
func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes < kb:
		return fmt.Sprintf("%d B", bytes)
	case bytes < mb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	case bytes < gb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	default:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gb)
	}
}

// getFileGroupSummary returns a short summary of tag groups for a file
func getFileGroupSummary(fi *FileInfo, cat string) string {
	if fi == nil || cat == "" {
		return ""
	}
	catTags := fi.CategoryTags(cat)
	if catTags == nil || len(catTags) == 0 {
		return ""
	}
	return fmt.Sprintf("(%d groups)", len(catTags))
}

// getCurrentFilePackageDir returns the package directory of the currently selected tree node
func (app *AppState) getCurrentFilePackageDir() string {
	if len(app.TreeFlat) == 0 || app.TreeState.Cursor >= len(app.TreeFlat) {
		return ""
	}

	node := app.TreeFlat[app.TreeState.Cursor]
	if node.IsDir {
		return node.Path
	}

	if node.FileInfo != nil {
		dir := filepath.Dir(node.Path)
		return filepath.ToSlash(dir)
	}

	return ""
}

// getCurrentFileInfo returns the FileInfo of the currently selected tree node
func (app *AppState) getCurrentFileInfo() *FileInfo {
	if len(app.TreeFlat) == 0 || app.TreeState.Cursor >= len(app.TreeFlat) {
		return nil
	}

	node := app.TreeFlat[app.TreeState.Cursor]
	if node.IsDir {
		return nil
	}

	return node.FileInfo
}

// countFilesInGroup returns the number of files with any tag in the given group
func (app *AppState) countFilesInGroup(cat, group string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if _, ok := fi.CategoryTags(cat)[group]; ok {
			count++
		}
	}
	return count
}

// countFilesInModule returns the number of files with any tag in the given module
func (app *AppState) countFilesInModule(cat, group, module string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				count++
			}
		}
	}
	return count
}

// countFilesWithTag returns the number of files with a specific tag
func (app *AppState) countFilesWithTag(cat, group, module, tag string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if tags, ok := mods[module]; ok {
				for _, t := range tags {
					if t == tag {
						count++
						break
					}
				}
			}
		}
	}
	return count
}

// countDirSelection returns (selected, total) file counts for a directory
func (app *AppState) countDirSelection(node *TreeNode) (int, int) {
	if !node.IsDir {
		return 0, 0
	}

	selected := 0
	total := 0

	var count func(n *TreeNode)
	count = func(n *TreeNode) {
		if n.IsDir {
			for _, child := range n.Children {
				count(child)
			}
		} else {
			total++
			if app.Selected[n.Path] {
				selected++
			}
		}
	}

	count(node)
	return selected, total
}

// computeDepExpandedFiles returns files included via dependency expansion
func (app *AppState) computeDepExpandedFiles() map[string]bool {
	result := make(map[string]bool)

	selectedDirs := make(map[string]bool)
	for path := range app.Selected {
		dir := filepath.Dir(path)
		dir = filepath.ToSlash(dir)
		if dir == "." {
			if fi, ok := app.Index.Files[path]; ok {
				dir = fi.Package
			}
		}
		selectedDirs[dir] = true
	}

	expandedDirs := ExpandDeps(selectedDirs, app.Index, app.DepthLimit)

	for dir := range selectedDirs {
		delete(expandedDirs, dir)
	}

	for dir := range expandedDirs {
		if pkg, ok := app.Index.Packages[dir]; ok {
			for _, f := range pkg.Files {
				if !app.Selected[f.Path] {
					result[f.Path] = true
				}
			}
		}
	}

	return result
}

// nodeMatchesFilter checks if a tree node matches the current filter
func (app *AppState) nodeMatchesFilter(node *TreeNode) bool {
	if !app.Filter.HasActiveFilter() {
		return true
	}

	if node.IsDir {
		for _, child := range node.Children {
			if app.nodeMatchesFilter(child) {
				return true
			}
		}
		return false
	}

	return app.Filter.FilteredPaths[node.Path]
}