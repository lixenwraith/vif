package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Render draws the complete UI to terminal based on current mode
func (app *AppState) Render() {
	w, h := app.Width, app.Height
	if w < minWidth {
		w = minWidth
	}
	if h < minHeight {
		h = minHeight
	}

	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	if app.DiveMode {
		app.RenderDive(cells, w, h)
	} else if app.MindmapMode {
		app.RenderMindmap(cells, w, h)
	} else if app.PreviewMode {
		app.renderPreview(cells, w, h)
	} else {
		app.renderSplitPane(cells, w, h)
	}

	app.Term.Flush(cells, w, h)
}

// renderSplitPane draws the two-pane main view layout
func (app *AppState) renderSplitPane(cells []terminal.Cell, w, h int) {
	// Calculate pane widths
	leftWidth := w / 2
	if leftWidth < leftPaneMin {
		leftWidth = leftPaneMin
	}
	rightWidth := w - leftWidth - 1 // -1 for border
	if rightWidth < rightPaneMin {
		rightWidth = rightPaneMin
		leftWidth = w - rightWidth - 1
	}

	// Header
	drawRect(cells, 0, 0, w, headerHeight, w, colorHeaderBg)
	title := "FOCUS-CATALOG"
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// Right side header info
	depsStr := fmt.Sprintf("Deps: %d", app.DepthLimit)
	if !app.ExpandDeps {
		depsStr = "Deps: OFF"
	}
	outputFiles := app.ComputeOutputFiles()
	rightInfo := fmt.Sprintf("%s  Output: %d files", depsStr, len(outputFiles))
	drawText(cells, w, w-len(rightInfo)-2, 0, rightInfo, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	// Content area
	contentTop := headerHeight
	contentHeight := h - headerHeight - statusHeight - helpHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Left pane background
	leftBg := colorDefaultBg
	if app.FocusPane == PaneLeft {
		leftBg = colorPaneActiveBg
	}
	drawRect(cells, 0, contentTop, leftWidth, contentHeight, w, leftBg)

	// Right pane background
	rightBg := colorDefaultBg
	if app.FocusPane == PaneRight {
		rightBg = colorPaneActiveBg
	}
	drawRect(cells, leftWidth+1, contentTop, rightWidth, contentHeight, w, rightBg)

	// Vertical border
	for y := contentTop; y < contentTop+contentHeight; y++ {
		cells[y*w+leftWidth] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	// Pane headers
	leftHeader := "PACKAGES / FILES"
	rightHeader := "GROUPS & TAGS"
	drawText(cells, w, 1, contentTop, leftHeader, colorStatusFg, leftBg, terminal.AttrBold)
	drawText(cells, w, leftWidth+2, contentTop, rightHeader, colorStatusFg, rightBg, terminal.AttrBold)

	// Render pane contents
	paneContentTop := contentTop + 1
	paneContentHeight := contentHeight - 1

	app.renderLeftPane(cells, w, 0, leftWidth, paneContentTop, paneContentHeight)
	app.renderRightPane(cells, w, leftWidth+1, rightWidth, paneContentTop, paneContentHeight)

	// Status area
	statusY := h - statusHeight - helpHeight
	app.renderStatus(cells, w, statusY)

	// Help bar
	helpY := h - helpHeight
	app.renderHelp(cells, w, helpY)
}

// renderLeftPane draws the tree pane with files and directories
func (app *AppState) renderLeftPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneLeft {
		bg = colorPaneActiveBg
	}

	// Get expanded file set for dependency highlighting
	depExpanded := make(map[string]bool)
	if app.ExpandDeps && len(app.Selected) > 0 {
		depExpanded = app.computeDepExpandedFiles()
	}

	for i := 0; i < height && app.TreeScroll+i < len(app.TreeFlat); i++ {
		y := startY + i
		idx := app.TreeScroll + i
		node := app.TreeFlat[idx]

		isCursor := idx == app.TreeCursor && app.FocusPane == PaneLeft
		rowBg := bg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Check if node matches filters (dim if not)
		matchesFilter := app.nodeMatchesFilter(node)
		dimmed := !matchesFilter && app.hasActiveFilters()

		// Clear line
		for x := startX; x < startX+paneWidth; x++ {
			cells[y*totalWidth+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
		}

		// Indentation
		indent := node.Depth * 2
		x := startX + 1 + indent

		if node.IsDir {
			// Directory: show expand indicator and name
			expandChar := '▶'
			if node.Expanded {
				expandChar = '▼'
			}
			dirFg := colorDirFg
			if dimmed {
				dirFg = colorUnselected
			}
			if x < startX+paneWidth-1 {
				cells[y*totalWidth+x] = terminal.Cell{Rune: expandChar, Fg: dirFg, Bg: rowBg}
			}
			x += 2

			// Selection count for directories
			selCount, totalCount := app.countDirSelection(node)
			nameStr := node.Name
			if totalCount > 0 {
				nameStr = fmt.Sprintf("%s [%d/%d]", node.Name, selCount, totalCount)
			}

			maxNameLen := paneWidth - indent - 5
			if len(nameStr) > maxNameLen && maxNameLen > 3 {
				nameStr = nameStr[:maxNameLen-1] + "…"
			}

			drawText(cells, totalWidth, x, y, nameStr, dirFg, rowBg, terminal.AttrNone)

		} else {
			// File: show checkbox and name
			isSelected := app.Selected[node.Path]
			isDepExpanded := depExpanded[node.Path]

			checkbox := "[ ]"
			checkFg := colorUnselected
			if isSelected {
				checkbox = "[x]"
				checkFg = colorSelected
			} else if isDepExpanded {
				checkbox = "[+]"
				checkFg = colorExpandedFg
			}

			if dimmed {
				checkFg = colorUnselected
			}

			if x+3 < startX+paneWidth {
				drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			}
			x += 4

			// File name
			nameFg := colorDefaultFg
			if node.FileInfo != nil && node.FileInfo.IsAll {
				nameFg = colorAllTagFg
			}
			if dimmed {
				nameFg = colorUnselected
			}

			nameStr := node.Name
			attr := terminal.AttrNone
			if dimmed {
				attr = terminal.AttrDim
			}
			drawText(cells, totalWidth, x, y, nameStr, nameFg, rowBg, attr)
			x += len(nameStr)

			// Group hints - flow after filename with gap
			if node.FileInfo != nil {
				groupHint := getFileGroupSummary(node.FileInfo)
				if groupHint != "" {
					remaining := (startX + paneWidth) - x - 2
					if remaining > 4 {
						if len(groupHint) > remaining {
							groupHint = groupHint[:remaining-1] + "…"
						}
						hintFg := colorGroupHintFg
						if dimmed {
							hintFg = colorUnselected
						}
						drawText(cells, totalWidth, x+2, y, groupHint, hintFg, rowBg, terminal.AttrDim)
					}
				}
			}
		}
	}
}

// renderRightPane draws the tag pane with groups and tags
func (app *AppState) renderRightPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneRight {
		bg = colorPaneActiveBg
	}

	hasFilter := app.Filter.HasActiveFilter()

	for i := 0; i < height && app.TagScroll+i < len(app.TagFlat); i++ {
		y := startY + i
		idx := app.TagScroll + i
		item := app.TagFlat[idx]

		isCursor := idx == app.TagCursor && app.FocusPane == PaneRight
		rowBg := bg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Clear line
		for x := startX; x < startX+paneWidth; x++ {
			cells[y*totalWidth+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
		}

		x := startX + 1

		if item.IsGroup {
			// Check filter and selection state
			isFiltered := app.isGroupFiltered(item.Group)
			dimmed := hasFilter && !isFiltered
			selState := app.computeGroupSelectionState(item.Group)

			// Expand indicator
			expandChar := '▶'
			if item.Expanded {
				expandChar = '▼'
			}
			expandFg := colorGroupFg
			if dimmed {
				expandFg = colorUnselected
			}
			cells[y*totalWidth+x] = terminal.Cell{Rune: expandChar, Fg: expandFg, Bg: rowBg}
			x += 2

			// Selection indicator
			checkbox := "[ ]"
			checkFg := colorUnselected
			switch selState {
			case TagSelectFull:
				checkbox = "[x]"
				checkFg = colorSelected
			case TagSelectPartial:
				checkbox = "[o]"
				checkFg = colorPartialSelectFg
			}
			if dimmed {
				checkFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			x += 4

			// Group name and count
			groupFg := colorGroupFg
			if dimmed {
				groupFg = colorUnselected
			}
			groupStr := "#" + item.Group
			drawText(cells, totalWidth, x, y, groupStr, groupFg, rowBg, terminal.AttrBold)
			x += len(groupStr)

			fileCount := app.countFilesInGroup(item.Group)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)
			x += len(countStr)

			// Directory hints
			dirs := app.getGroupDirectories(item.Group)
			if len(dirs) > 0 {
				remaining := (startX + paneWidth) - x - 2
				if remaining > 4 {
					dirHint := formatDirHints(dirs, remaining)
					if dirHint != "" {
						hintFg := colorDirHintFg
						if dimmed {
							hintFg = colorUnselected
						}
						drawText(cells, totalWidth, x+2, y, dirHint, hintFg, rowBg, terminal.AttrDim)
					}
				}
			}
		} else {
			// Tag item (indented)
			isFiltered := app.isTagFiltered(item.Group, item.Tag)
			dimmed := hasFilter && !isFiltered
			selState := app.computeTagSelectionState(item.Group, item.Tag)

			x += 4

			checkbox := "[ ]"
			checkFg := colorUnselected
			switch selState {
			case TagSelectFull:
				checkbox = "[x]"
				checkFg = colorSelected
			case TagSelectPartial:
				checkbox = "[o]"
				checkFg = colorPartialSelectFg
			}
			if dimmed {
				checkFg = colorUnselected
			}

			drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			x += 4

			tagFg := colorTagFg
			if dimmed {
				tagFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, item.Tag, tagFg, rowBg, terminal.AttrNone)
			x += len(item.Tag)

			fileCount := app.countFilesWithTag(item.Group, item.Tag)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)
			x += len(countStr)

			// Directory hints
			dirs := app.getTagDirectories(item.Group, item.Tag)
			if len(dirs) > 0 {
				remaining := (startX + paneWidth) - x - 2
				if remaining > 4 {
					dirHint := formatDirHints(dirs, remaining)
					if dirHint != "" {
						hintFg := colorDirHintFg
						if dimmed {
							hintFg = colorUnselected
						}
						drawText(cells, totalWidth, x+2, y, dirHint, hintFg, rowBg, terminal.AttrDim)
					}
				}
			}
		}
	}
}

// renderStatus draws filter/input status and message lines
func (app *AppState) renderStatus(cells []terminal.Cell, w, y int) {
	// Line 1: Filter info or edit mode
	if app.EditMode {
		label := fmt.Sprintf("Edit [%s]: ", app.EditTarget)
		maxInputLen := w - len(label) - 3
		input := app.InputBuffer
		if len(input) > maxInputLen && maxInputLen > 3 {
			input = input[len(input)-maxInputLen+3:]
		}
		editStr := label + input + "_"
		drawText(cells, w, 1, y, editStr, colorHeaderFg, colorInputBg, terminal.AttrNone)
		drawText(cells, w, 1, y+1, "Enter:save  Esc:cancel", colorHelpFg, colorDefaultBg, terminal.AttrDim)
		return
	}

	if app.InputMode {
		typeHint := "content"
		switch app.SearchType {
		case SearchTypeTags:
			typeHint = "tags"
		case SearchTypeGroups:
			typeHint = "groups"
		}
		inputStr := fmt.Sprintf("Search [%s]: %s_", typeHint, app.InputBuffer)
		drawText(cells, w, 1, y, inputStr, colorHeaderFg, colorInputBg, terminal.AttrNone)
	} else if app.Filter.HasActiveFilter() {
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
		drawText(cells, w, 1, y, filterStr, colorTagFg, colorDefaultBg, terminal.AttrNone)
	}

	// Line 2: Message or selection info
	statusStr := app.Message
	if statusStr == "" {
		selCount := len(app.Selected)
		statusStr = fmt.Sprintf("Selected: %d files", selCount)
	}
	drawText(cells, w, 1, y+1, statusStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
}

// renderHelp draws the keybinding help bar
func (app *AppState) renderHelp(cells []terminal.Cell, w, y int) {
	help := "Tab:pane  j/k:nav 0/$:jump Space:sel  f:filter F:sel-filter /:search  t:tag  g:group  m:mode  d:deps  Enter:view  ^S:output  Esc:clear  q:quit"
	if len(help) > w-2 {
		help = help[:w-5] + "..."
	}
	drawText(cells, w, 1, y, help, colorHelpFg, colorDefaultBg, terminal.AttrDim)
}

// renderPreview draws the output file preview overlay
func (app *AppState) renderPreview(cells []terminal.Cell, w, h int) {
	// Header
	drawRect(cells, 0, 0, w, 1, w, colorHeaderBg)
	title := fmt.Sprintf("PREVIEW (%d files) - press p/q/Esc to close", len(app.PreviewFiles))
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// File list
	for i := 1; i < h-1; i++ {
		idx := app.PreviewScroll + i - 1
		if idx >= len(app.PreviewFiles) {
			break
		}
		drawText(cells, w, 1, i, "./"+app.PreviewFiles[idx], colorDefaultFg, colorDefaultBg, terminal.AttrNone)
	}

	// Scroll indicator
	if len(app.PreviewFiles) > h-2 {
		pct := 0
		if len(app.PreviewFiles) > 0 {
			pct = (app.PreviewScroll * 100) / len(app.PreviewFiles)
		}
		scrollStr := fmt.Sprintf("[%d%%]", pct)
		drawText(cells, w, w-len(scrollStr)-1, h-1, scrollStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
	}
}

// countDirSelection counts selected and total files under directory
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

// countFilesInGroup counts files having any tag in specified group
func (app *AppState) countFilesInGroup(group string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if _, ok := fi.Focus[group]; ok {
			count++
		}
	}
	return count
}

// RefreshTreeFlat rebuilds flattened tree list from root
func (app *AppState) RefreshTreeFlat() {
	app.TreeFlat = FlattenTree(app.TreeRoot)

	// Bounds check cursor
	if app.TreeCursor >= len(app.TreeFlat) {
		app.TreeCursor = len(app.TreeFlat) - 1
	}
	if app.TreeCursor < 0 {
		app.TreeCursor = 0
	}
}

// RefreshTagFlat rebuilds flattened tag list from index
func (app *AppState) RefreshTagFlat() {
	app.TagFlat = nil

	for _, group := range app.Index.FocusGroups {
		expanded := true
		if exp, ok := app.GroupExpanded[group]; ok {
			expanded = exp
		} else {
			app.GroupExpanded[group] = true
		}

		app.TagFlat = append(app.TagFlat, TagItem{
			IsGroup:  true,
			Group:    group,
			Expanded: expanded,
		})

		if expanded {
			if tags, ok := app.Index.FocusTags[group]; ok {
				for _, tag := range tags {
					app.TagFlat = append(app.TagFlat, TagItem{
						IsGroup: false,
						Group:   group,
						Tag:     tag,
					})
				}
			}
		}
	}

	if app.TagCursor >= len(app.TagFlat) {
		app.TagCursor = len(app.TagFlat) - 1
	}
	if app.TagCursor < 0 {
		app.TagCursor = 0
	}
}

// computeDepExpandedFiles returns files included via dependency expansion
func (app *AppState) computeDepExpandedFiles() map[string]bool {
	result := make(map[string]bool)

	// Get package directories from selected files
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

	// Expand dependencies
	expandedDirs := ExpandDeps(selectedDirs, app.Index, app.DepthLimit)

	// Remove originally selected directories
	for dir := range selectedDirs {
		delete(expandedDirs, dir)
	}

	// Collect files from expanded packages
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

// hasActiveFilters returns true if filter state is non-empty
func (app *AppState) hasActiveFilters() bool {
	return app.Filter.HasActiveFilter()
}

// nodeMatchesFilter checks if tree node or descendants match filter
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

// countFilesWithTag counts files having specific tag
func (app *AppState) countFilesWithTag(group, tag string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if tags, ok := fi.Focus[group]; ok {
			for _, t := range tags {
				if t == tag {
					count++
					break
				}
			}
		}
	}
	return count
}

// getFileGroupSummary formats file's groups as "group(count)" string
func getFileGroupSummary(fi *FileInfo) string {
	if fi == nil || len(fi.Focus) == 0 {
		return ""
	}

	// Collect groups sorted alphabetically
	groups := make([]string, 0, len(fi.Focus))
	for g := range fi.Focus {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var parts []string
	for _, g := range groups {
		count := len(fi.Focus[g])
		parts = append(parts, fmt.Sprintf("%s(%d)", g, count))
	}

	return strings.Join(parts, " ")
}

// getTagDirectories returns base directory names for files with tag
func (app *AppState) getTagDirectories(group, tag string) []string {
	dirSet := make(map[string]bool)

	for path, fi := range app.Index.Files {
		if tags, ok := fi.Focus[group]; ok {
			for _, t := range tags {
				if t == tag {
					dir := filepath.Dir(path)
					if dir == "." {
						dir = fi.Package
					}
					dirSet[filepath.Base(dir)] = true
					break
				}
			}
		}
	}

	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// getGroupDirectories returns base directory names for files in group
func (app *AppState) getGroupDirectories(group string) []string {
	dirSet := make(map[string]bool)

	for path, fi := range app.Index.Files {
		if _, ok := fi.Focus[group]; ok {
			dir := filepath.Dir(path)
			if dir == "." {
				dir = fi.Package
			}
			dirSet[filepath.Base(dir)] = true
		}
	}

	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}