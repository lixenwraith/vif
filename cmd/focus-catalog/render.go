package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Render draws the entire UI
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

	if app.MindmapMode {
		app.RenderMindmap(cells, w, h)
	} else if app.PreviewMode {
		app.renderPreview(cells, w, h)
	} else {
		app.renderSplitPane(cells, w, h)
	}

	app.Term.Flush(cells, w, h)
}

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
		cells[y*w+leftWidth] = terminal.Cell{Rune: '│', Fg: colorPaneBorder, Bg: colorDefaultBg}
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

			maxNameLen := paneWidth - indent - 8
			nameStr := node.Name
			if len(nameStr) > maxNameLen && maxNameLen > 3 {
				nameStr = nameStr[:maxNameLen-1] + "…"
			}

			attr := terminal.AttrNone
			if dimmed {
				attr = terminal.AttrDim
			}
			drawText(cells, totalWidth, x, y, nameStr, nameFg, rowBg, attr)
		}
	}
}

func (app *AppState) renderRightPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneRight {
		bg = colorPaneActiveBg
	}

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
			// Expand indicator
			expandChar := '▶'
			if item.Expanded {
				expandChar = '▼'
			}
			cells[y*totalWidth+x] = terminal.Cell{Rune: expandChar, Fg: colorGroupFg, Bg: rowBg}
			x += 2

			// Selection indicator
			indicator := "○"
			indicatorFg := colorUnselected
			if item.HasSelected {
				indicator = "◉"
				indicatorFg = colorSelected
			}
			drawText(cells, totalWidth, x, y, indicator, indicatorFg, rowBg, terminal.AttrNone)
			x += 2

			groupStr := "#" + item.Group
			drawText(cells, totalWidth, x, y, groupStr, colorGroupFg, rowBg, terminal.AttrBold)

			// Show tag count
			if tags, ok := app.Index.AllTags[item.Group]; ok {
				countStr := fmt.Sprintf(" (%d)", len(tags))
				drawText(cells, totalWidth, x+len(groupStr), y, countStr, colorStatusFg, rowBg, terminal.AttrNone)
			}
		} else {
			// Tag item (indented)
			x += 4

			checkbox := "[ ]"
			checkFg := colorUnselected
			if item.Selected {
				checkbox = "[x]"
				checkFg = colorSelected
			}

			drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			x += 4

			drawText(cells, totalWidth, x, y, item.Tag, colorTagFg, rowBg, terminal.AttrNone)

			// Show file count for this tag
			fileCount := app.countFilesWithTag(item.Group, item.Tag)
			if fileCount > 0 {
				countStr := fmt.Sprintf(" (%d)", fileCount)
				drawText(cells, totalWidth, x+len(item.Tag), y, countStr, colorStatusFg, rowBg, terminal.AttrNone)
			}
		}
	}
}

func (app *AppState) renderStatus(cells []terminal.Cell, w, y int) {
	// Line 1: Filter info or edit mode
	if app.EditMode {
		// Edit mode UI
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

	var filterParts []string

	if app.Filter.HasSelectedTags() {
		tagCount := app.Filter.SelectedTagCount()
		modeStr := "OR"
		if app.Filter.Mode == FilterAND {
			modeStr = "AND"
		}
		filterParts = append(filterParts, fmt.Sprintf("Tags: %d (%s)", tagCount, modeStr))
	}

	if app.Filter.Keyword != "" {
		matchCount := len(app.Filter.KeywordMatch)
		searchStr := "meta"
		if app.Filter.SearchMode == SearchModeContent {
			searchStr = "content"
		}
		filterParts = append(filterParts, fmt.Sprintf("Keyword: %q (%d, %s)", app.Filter.Keyword, matchCount, searchStr))
	}

	if len(filterParts) > 0 {
		filterStr := strings.Join(filterParts, "  |  ")
		drawText(cells, w, 1, y, filterStr, colorTagFg, colorDefaultBg, terminal.AttrNone)
	} else if app.InputMode {
		searchStr := "[meta]"
		if app.Filter.SearchMode == SearchModeContent {
			searchStr = "[content]"
		}
		inputStr := fmt.Sprintf("Search %s: %s_", searchStr, app.InputBuffer)
		drawText(cells, w, 1, y, inputStr, colorHeaderFg, colorInputBg, terminal.AttrNone)
	}

	// Line 2: Message or selection info
	statusStr := app.Message
	if statusStr == "" {
		selCount := len(app.Selected)
		statusStr = fmt.Sprintf("Selected: %d files", selCount)
	}
	drawText(cells, w, 1, y+1, statusStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
}

func (app *AppState) renderHelp(cells []terminal.Cell, w, y int) {
	help := "Tab:pane  j/k:nav  Space:sel  /:search  s:search-mode  m:filter-mode  d:deps  Enter:out  q:quit"
	if len(help) > w-2 {
		help = help[:w-5] + "..."
	}
	drawText(cells, w, 1, y, help, colorHelpFg, colorDefaultBg, terminal.AttrDim)
}

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

// drawText draws text at position
func drawText(cells []terminal.Cell, width, x, y int, text string, fg, bg terminal.RGB, attr terminal.Attr) {
	for i, r := range text {
		if x+i >= width || x+i < 0 {
			break
		}
		cells[y*width+x+i] = terminal.Cell{
			Rune:  r,
			Fg:    fg,
			Bg:    bg,
			Attrs: attr,
		}
	}
}

// drawRect fills a rectangle with background color
func drawRect(cells []terminal.Cell, startX, startY, rectW, rectH, totalWidth int, bg terminal.RGB) {
	for row := startY; row < startY+rectH; row++ {
		for col := startX; col < startX+rectW && col < totalWidth; col++ {
			idx := row*totalWidth + col
			if idx >= 0 && idx < len(cells) {
				cells[idx] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
			}
		}
	}
}

// RefreshTreeFlat rebuilds flattened tree list
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

// RefreshTagFlat rebuilds flattened tag list
func (app *AppState) RefreshTagFlat() {
	app.TagFlat = nil

	for _, group := range app.Index.Groups {
		// Check if group has any selected tags
		hasSelected := false
		if groupTags, ok := app.Filter.SelectedTags[group]; ok {
			for _, sel := range groupTags {
				if sel {
					hasSelected = true
					break
				}
			}
		}

		// Default to expanded if not set
		expanded := true
		if exp, ok := app.GroupExpanded[group]; ok {
			expanded = exp
		} else {
			app.GroupExpanded[group] = true
		}

		// Group header
		app.TagFlat = append(app.TagFlat, TagItem{
			IsGroup:     true,
			Group:       group,
			HasSelected: hasSelected,
			Expanded:    expanded,
		})

		// Tags in group (only if expanded)
		if expanded {
			if tags, ok := app.Index.AllTags[group]; ok {
				for _, tag := range tags {
					selected := false
					if groupTags, ok := app.Filter.SelectedTags[group]; ok {
						selected = groupTags[tag]
					}

					app.TagFlat = append(app.TagFlat, TagItem{
						IsGroup:  false,
						Group:    group,
						Tag:      tag,
						Selected: selected,
					})
				}
			}
		}
	}

	// Bounds check cursor
	if app.TagCursor >= len(app.TagFlat) {
		app.TagCursor = len(app.TagFlat) - 1
	}
	if app.TagCursor < 0 {
		app.TagCursor = 0
	}
}

// countDirSelection counts selected and total files in a directory
func (app *AppState) countDirSelection(node *TreeNode) (selected, total int) {
	if !node.IsDir {
		if app.Selected[node.Path] {
			return 1, 1
		}
		return 0, 1
	}

	for _, child := range node.Children {
		s, t := app.countDirSelection(child)
		selected += s
		total += t
	}
	return
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

// hasActiveFilters returns true if any filter is active
func (app *AppState) hasActiveFilters() bool {
	return app.Filter.HasSelectedTags() || app.Filter.Keyword != ""
}

// nodeMatchesFilter checks if a tree node matches current filters
func (app *AppState) nodeMatchesFilter(node *TreeNode) bool {
	if !app.hasActiveFilters() {
		return true
	}

	if node.IsDir {
		// Directory matches if any child matches
		for _, child := range node.Children {
			if app.nodeMatchesFilter(child) {
				return true
			}
		}
		return false
	}

	// File node
	if node.FileInfo == nil {
		return false
	}
	return app.FileMatchesAllFilters(node.FileInfo)
}

// countFilesWithTag counts files that have a specific tag
func (app *AppState) countFilesWithTag(group, tag string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if tags, ok := fi.Tags[group]; ok {
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