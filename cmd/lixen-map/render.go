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

	if app.HelpMode {
		app.RenderHelp(cells, w, h)
	} else if app.BatchEditMode {
		app.renderBatchEdit(cells, w, h)
	} else if app.PreviewMode {
		app.renderPreview(cells, w, h)
	} else {
		app.renderSplitPane(cells, w, h)
	}

	app.Term.Flush(cells, w, h)
}

// renderSplitPane draws the four-pane main view layout
func (app *AppState) renderSplitPane(cells []terminal.Cell, w, h int) {
	// Calculate pane widths - equal quarters
	paneWidth := (w - 3) / 4 // -3 for three vertical borders
	p1Width := paneWidth
	p2Width := paneWidth
	p3Width := paneWidth
	p4Width := w - p1Width - p2Width - p3Width - 3

	// Pane X positions
	p1Start := 0
	p2Start := p1Width + 1
	p3Start := p2Start + p2Width + 1
	p4Start := p3Start + p3Width + 1

	// Header row
	drawRect(cells, 0, 0, w, headerHeight, w, colorHeaderBg)
	title := "LIXEN-MAP"
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// Compute output stats
	totalFiles, depFiles, totalSize, depSize := app.computeOutputStats()

	// Determine size color based on threshold
	sizeFg := colorHeaderFg
	if totalSize > SizeWarningThreshold {
		sizeFg = terminal.RGB{R: 255, G: 80, B: 80}
	}

	// Right side header info
	x := w - 2

	if app.ExpandDeps && depSize > 0 {
		depSizeStr := fmt.Sprintf("(+%s dep)", formatSize(depSize))
		x -= len(depSizeStr)
		drawText(cells, w, x, 0, depSizeStr, colorStatusFg, colorHeaderBg, terminal.AttrNone)
		x--
	}

	sizeVal := formatSize(totalSize)
	x -= len(sizeVal)
	drawText(cells, w, x, 0, sizeVal, sizeFg, colorHeaderBg, terminal.AttrNone)

	sizeLabel := "Size: "
	x -= len(sizeLabel)
	drawText(cells, w, x, 0, sizeLabel, colorStatusFg, colorHeaderBg, terminal.AttrNone)

	sep1 := " | "
	x -= len(sep1)
	drawText(cells, w, x, 0, sep1, colorPaneBorder, colorHeaderBg, terminal.AttrNone)

	filesVal := fmt.Sprintf("%d files", totalFiles)
	x -= len(filesVal)
	drawText(cells, w, x, 0, filesVal, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	filesLabel := "Output: "
	x -= len(filesLabel)
	drawText(cells, w, x, 0, filesLabel, colorStatusFg, colorHeaderBg, terminal.AttrNone)

	sep2 := " | "
	x -= len(sep2)
	drawText(cells, w, x, 0, sep2, colorPaneBorder, colorHeaderBg, terminal.AttrNone)

	var depsVal string
	if app.ExpandDeps {
		depsVal = fmt.Sprintf("%d (+%d)", depFiles, app.DepthLimit)
	} else {
		depsVal = "OFF"
	}
	x -= len(depsVal)
	drawText(cells, w, x, 0, depsVal, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	depsLabel := "Deps: "
	x -= len(depsLabel)
	drawText(cells, w, x, 0, depsLabel, colorStatusFg, colorHeaderBg, terminal.AttrNone)

	// Content area
	contentTop := headerHeight
	contentHeight := h - headerHeight - statusHeight - helpHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Pane backgrounds based on focus
	p1Bg := colorDefaultBg
	if app.FocusPane == PaneLixen {
		p1Bg = colorPaneActiveBg
	}
	drawRect(cells, p1Start, contentTop, p1Width, contentHeight, w, p1Bg)

	p2Bg := colorDefaultBg
	if app.FocusPane == PaneTree {
		p2Bg = colorPaneActiveBg
	}
	drawRect(cells, p2Start, contentTop, p2Width, contentHeight, w, p2Bg)

	p3Bg := colorDefaultBg
	if app.FocusPane == PaneDepBy {
		p3Bg = colorPaneActiveBg
	}
	drawRect(cells, p3Start, contentTop, p3Width, contentHeight, w, p3Bg)

	p4Bg := colorDefaultBg
	if app.FocusPane == PaneDepOn {
		p4Bg = colorPaneActiveBg
	}
	drawRect(cells, p4Start, contentTop, p4Width, contentHeight, w, p4Bg)

	// Vertical borders
	for y := contentTop; y < contentTop+contentHeight; y++ {
		cells[y*w+p1Width] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		cells[y*w+p2Start+p2Width] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		cells[y*w+p3Start+p3Width] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	// Pane headers centered on top border line
	p1Header := app.formatLixenHeader()
	p2Header := app.formatTreeHeader()
	p3Header := "DEPENDED BY"
	p4Header := "DEPENDS ON"

	drawPaneHeader(cells, w, p1Start, contentTop, p1Width, p1Header, p1Bg)
	drawPaneHeader(cells, w, p2Start, contentTop, p2Width, p2Header, p2Bg)
	drawPaneHeader(cells, w, p3Start, contentTop, p3Width, p3Header, p3Bg)
	drawPaneHeader(cells, w, p4Start, contentTop, p4Width, p4Header, p4Bg)

	// Render pane contents (below header line)
	paneContentTop := contentTop + 1
	paneContentHeight := contentHeight - 1

	app.renderLixenPane(cells, w, p1Start, p1Width, paneContentTop, paneContentHeight)
	app.renderTreePane(cells, w, p2Start, p2Width, paneContentTop, paneContentHeight)
	app.renderDepByPane(cells, w, p3Start, p3Width, paneContentTop, paneContentHeight)
	app.renderDepOnPane(cells, w, p4Start, p4Width, paneContentTop, paneContentHeight)

	// Status area
	statusY := h - statusHeight - helpHeight
	app.renderStatus(cells, w, statusY)

	// Help bar
	helpY := h - helpHeight
	app.renderHelpBar(cells, w, helpY)
}

// drawPaneHeader draws centered header text on the top border line of a pane
func drawPaneHeader(cells []terminal.Cell, totalW, paneX, y, paneW int, header string, bg terminal.RGB) {
	// Truncate if too long
	maxLen := paneW - 4
	if len(header) > maxLen && maxLen > 3 {
		header = header[:maxLen-1] + "…"
	}

	// Center the header
	headerX := paneX + (paneW-len(header))/2
	if headerX < paneX+1 {
		headerX = paneX + 1
	}

	drawText(cells, totalW, headerX, y, header, colorStatusFg, bg, terminal.AttrBold)
}

// formatLixenHeader returns header for lixen pane with category name
func (app *AppState) formatLixenHeader() string {
	if app.CurrentCategory == "" {
		return "LIXEN"
	}
	return fmt.Sprintf("LIXEN: %s", app.CurrentCategory)
}

// formatTreeHeader returns header for tree pane with path truncation hint
func (app *AppState) formatTreeHeader() string {
	return "PACKAGES / FILES"
}

// renderLixenPane draws the category tag pane (left)
func (app *AppState) renderLixenPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneLixen {
		bg = colorPaneActiveBg
	}

	cat := app.CurrentCategory
	ui := app.getCurrentCategoryUI()
	if ui == nil || cat == "" {
		// No category - show placeholder
		msg := "(no categories)"
		drawText(cells, totalWidth, startX+2, startY, msg, colorUnselected, bg, terminal.AttrDim)
		return
	}

	hasFilter := app.Filter.HasActiveFilter()

	for i := 0; i < height && ui.Scroll+i < len(ui.Flat); i++ {
		y := startY + i
		idx := ui.Scroll + i
		item := ui.Flat[idx]

		isCursor := idx == ui.Cursor && app.FocusPane == PaneLixen
		rowBg := bg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Clear line
		for x := startX; x < startX+paneWidth; x++ {
			cells[y*totalWidth+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
		}

		x := startX + 1

		switch item.Type {
		case TagItemTypeGroup:
			isFiltered := app.isGroupFiltered(cat, item.Group)
			dimmed := hasFilter && !isFiltered
			selState := app.computeGroupSelectionState(cat, item.Group)

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

			groupFg := colorGroupFg
			if dimmed {
				groupFg = colorUnselected
			}
			groupStr := "#" + item.Group
			drawText(cells, totalWidth, x, y, groupStr, groupFg, rowBg, terminal.AttrBold)
			x += len(groupStr)

			fileCount := app.countFilesInGroup(cat, item.Group)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeModule:
			isFiltered := app.isModuleFiltered(cat, item.Group, item.Module)
			dimmed := hasFilter && !isFiltered
			selState := app.computeModuleSelectionState(cat, item.Group, item.Module)

			x += 2 // indent under group

			expandChar := '▶'
			if item.Expanded {
				expandChar = '▼'
			}
			expandFg := colorModuleFg
			if dimmed {
				expandFg = colorUnselected
			}
			cells[y*totalWidth+x] = terminal.Cell{Rune: expandChar, Fg: expandFg, Bg: rowBg}
			x += 2

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

			moduleFg := colorModuleFg
			if dimmed {
				moduleFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, item.Module, moduleFg, rowBg, terminal.AttrNone)
			x += len(item.Module)

			fileCount := app.countFilesInModule(cat, item.Group, item.Module)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeTag:
			isFiltered := app.isTagFiltered(cat, item.Group, item.Module, item.Tag)
			dimmed := hasFilter && !isFiltered
			selState := app.computeTagSelectionState(cat, item.Group, item.Module, item.Tag)

			// Indent: 2 for group, +2 for module (if not direct), +4 for checkbox
			indent := 4
			if item.Module != DirectTagsModule {
				indent = 6
			}
			x += indent

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

			fileCount := app.countFilesWithTag(cat, item.Group, item.Module, item.Tag)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)
		}
	}
}

func (app *AppState) renderTreePane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneTree {
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

		isCursor := idx == app.TreeCursor && app.FocusPane == PaneTree
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
			// Directory: show checkbox, expand indicator, and name
			selCount, totalCount := app.countDirSelection(node)

			// Expand indicator
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

			// Selection checkbox
			checkbox := "[ ]"
			checkFg := colorUnselected
			if totalCount > 0 {
				if selCount == totalCount {
					checkbox = "[x]"
					checkFg = colorSelected
				} else if selCount > 0 {
					checkbox = "[o]"
					checkFg = colorPartialSelectFg
				}
			}
			if dimmed {
				checkFg = colorUnselected
			}
			if x+3 < startX+paneWidth {
				drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			}
			x += 4

			// Directory name with count
			nameStr := node.Name
			if totalCount > 0 {
				nameStr = fmt.Sprintf("%s [%d/%d]", node.Name, selCount, totalCount)
			}

			maxNameLen := paneWidth - indent - 9 // checkbox(4) + expand(2) + margin
			if len(nameStr) > maxNameLen && maxNameLen > 3 {
				nameStr = nameStr[:maxNameLen-1] + "â€¦"
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
				groupHint := getFileGroupSummary(node.FileInfo, app.CurrentCategory)
				if groupHint != "" {
					remaining := (startX + paneWidth) - x - 2
					if remaining > 4 {
						if len(groupHint) > remaining {
							groupHint = groupHint[:remaining-1] + "â€¦"
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

// renderDepByPane draws packages/files that depend on current file's package
func (app *AppState) renderDepByPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneDepBy {
		bg = colorPaneActiveBg
	}

	pkgDir := app.getCurrentFilePackageDir()
	if pkgDir == "" {
		msg := "(select a file)"
		drawText(cells, totalWidth, startX+2, startY, msg, colorUnselected, bg, terminal.AttrDim)
		return
	}

	state := app.DepByState
	if len(state.FlatItems) == 0 {
		msg := "(no dependents)"
		drawText(cells, totalWidth, startX+2, startY, msg, colorUnselected, bg, terminal.AttrDim)
		return
	}

	app.renderDetailList(cells, totalWidth, startX, paneWidth, startY, height, bg, state)
}

// renderDepOnPane draws files/packages that current file depends on
func (app *AppState) renderDepOnPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneDepOn {
		bg = colorPaneActiveBg
	}

	fi := app.getCurrentFileInfo()
	if fi == nil {
		msg := "(select a file)"
		drawText(cells, totalWidth, startX+2, startY, msg, colorUnselected, bg, terminal.AttrDim)
		return
	}

	state := app.DepOnState
	if len(state.FlatItems) == 0 {
		msg := "(no imports/analysis)"
		drawText(cells, totalWidth, startX+2, startY, msg, colorUnselected, bg, terminal.AttrDim)
		return
	}

	app.renderDetailList(cells, totalWidth, startX, paneWidth, startY, height, bg, state)
}

// renderDetailList generic renderer for detail panes
func (app *AppState) renderDetailList(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int, bg terminal.RGB, state *DetailPaneState) {
	for i := 0; i < height && state.Scroll+i < len(state.FlatItems); i++ {
		y := startY + i
		idx := state.Scroll + i
		item := state.FlatItems[idx]

		isCursor := idx == state.Cursor && (app.FocusPane == PaneDepBy || app.FocusPane == PaneDepOn)
		if state == app.DepByState && app.FocusPane != PaneDepBy {
			isCursor = false
		}
		if state == app.DepOnState && app.FocusPane != PaneDepOn {
			isCursor = false
		}

		rowBg := bg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Clear line
		for x := startX; x < startX+paneWidth; x++ {
			cells[y*totalWidth+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
		}

		x := startX + 1 + (item.Level * 2)

		// Icon/expand indicator
		if item.IsHeader {
			expandChar := '▶'
			if item.Expanded {
				expandChar = '▼'
			}
			iconFg := colorDirFg
			if !item.IsLocal {
				iconFg = colorExternalPkgFg
			}
			if x < startX+paneWidth-1 {
				cells[y*totalWidth+x] = terminal.Cell{Rune: expandChar, Fg: iconFg, Bg: rowBg}
			}
			x += 2

			// Selection checkbox for local headers
			if item.IsLocal && item.PkgDir != "" {
				checkbox, checkFg := app.getHeaderSelectionState(item.PkgDir)
				if x+3 < startX+paneWidth {
					drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
				}
				x += 4
			}
		} else if item.IsSymbol {
			if x < startX+paneWidth-1 {
				cells[y*totalWidth+x] = terminal.Cell{Rune: '•', Fg: colorSymbolFg, Bg: rowBg}
			}
			x += 2
		} else if item.IsFile {
			// Selection checkbox for files
			checkbox := "[ ]"
			checkFg := colorUnselected
			if app.Selected[item.Path] {
				checkbox = "[x]"
				checkFg = colorSelected
			}
			if x+3 < startX+paneWidth {
				drawText(cells, totalWidth, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			}
			x += 4

			// Usage indicator after checkbox
			if item.HasUsage {
				if x < startX+paneWidth-1 {
					cells[y*totalWidth+x] = terminal.Cell{Rune: '★', Fg: colorModuleFg, Bg: rowBg}
				}
				x += 2
			}
		}

		// Text
		fg := colorDefaultFg
		if item.IsHeader {
			if item.IsLocal {
				fg = colorModuleFg
			} else {
				fg = colorExternalPkgFg
			}
		} else if item.IsSymbol {
			fg = colorSymbolFg
		} else if item.IsFile {
			if item.HasUsage {
				fg = colorHeaderFg
			} else {
				fg = colorDefaultFg
			}
		}

		label := item.Label
		maxLen := (startX + paneWidth) - x - 1
		if len(label) > maxLen && maxLen > 3 {
			label = label[:maxLen-1] + "…"
		}

		attr := terminal.AttrNone
		if item.IsHeader || item.HasUsage {
			attr = terminal.AttrBold
		}

		drawText(cells, totalWidth, x, y, label, fg, rowBg, attr)
	}

	// Scroll indicator
	if len(state.FlatItems) > height {
		var scrollStr string
		if state.Scroll == 0 {
			scrollStr = "Top"
		} else if state.Scroll+height >= len(state.FlatItems) {
			scrollStr = "Bot"
		} else {
			pct := (state.Scroll * 100) / (len(state.FlatItems) - height)
			scrollStr = fmt.Sprintf("%d%%", pct)
		}

		drawText(cells, totalWidth, startX+paneWidth-len(scrollStr)-1, startY+height-1, scrollStr, colorStatusFg, bg, terminal.AttrDim)
	}
}

// getHeaderSelectionState returns checkbox string and color for package header
func (app *AppState) getHeaderSelectionState(pkgDir string) (string, terminal.RGB) {
	pkg := app.Index.Packages[pkgDir]
	if pkg == nil || len(pkg.Files) == 0 {
		return "[ ]", colorUnselected
	}

	selected := 0
	for _, fi := range pkg.Files {
		if app.Selected[fi.Path] {
			selected++
		}
	}

	if selected == 0 {
		return "[ ]", colorUnselected
	}
	if selected == len(pkg.Files) {
		return "[x]", colorSelected
	}
	return "[o]", colorPartialSelectFg
}

// getCurrentFilePackageDir returns the package directory of file at tree cursor
func (app *AppState) getCurrentFilePackageDir() string {
	if len(app.TreeFlat) == 0 || app.TreeCursor >= len(app.TreeFlat) {
		return ""
	}

	node := app.TreeFlat[app.TreeCursor]
	if node.IsDir {
		return node.Path
	}

	if node.FileInfo != nil {
		dir := filepath.Dir(node.Path)
		// Return strictly the directory path to match index keys
		return filepath.ToSlash(dir)
	}

	return ""
}

// getCurrentFileInfo returns FileInfo of file at tree cursor, nil if directory
func (app *AppState) getCurrentFileInfo() *FileInfo {
	if len(app.TreeFlat) == 0 || app.TreeCursor >= len(app.TreeFlat) {
		return nil
	}

	node := app.TreeFlat[app.TreeCursor]
	if node.IsDir {
		return nil
	}

	return node.FileInfo
}

// truncatePathLeft truncates path from left side, showing ".../" prefix
func truncatePathLeft(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	if maxLen <= 4 {
		return path[:maxLen]
	}

	// Find a good split point
	parts := strings.Split(path, "/")
	result := parts[len(parts)-1]

	for i := len(parts) - 2; i >= 0; i-- {
		candidate := parts[i] + "/" + result
		if len(candidate)+4 > maxLen { // 4 for ".../"
			break
		}
		result = candidate
	}

	if result == path {
		return path[:maxLen-1] + "…"
	}

	return ".../" + result
}

// renderStatus draws filter/input status and message lines
func (app *AppState) renderStatus(cells []terminal.Cell, w, y int) {
	// Line 1: Filter info or edit mode
	if app.EditMode {
		label := fmt.Sprintf("Edit [%s]: ", filepath.Base(app.EditTarget))
		maxInputLen := w - len(label) - 3

		input := app.InputBuffer
		cursor := app.EditCursor
		if len(input) > maxInputLen && maxInputLen > 3 {
			// Scroll view to keep cursor visible
			start := 0
			if cursor > maxInputLen-3 {
				start = cursor - maxInputLen + 3
			}
			end := start + maxInputLen
			if end > len(input) {
				end = len(input)
			}
			input = input[start:end]
			cursor = cursor - start
		}

		// Draw label
		drawText(cells, w, 1, y, label, colorHeaderFg, colorInputBg, terminal.AttrNone)

		// Draw input with cursor
		inputX := 1 + len(label)
		for i, r := range input {
			bg := colorInputBg
			if i == cursor {
				bg = colorCursorBg
			}
			if inputX+i < w-1 {
				cells[y*w+inputX+i] = terminal.Cell{Rune: r, Fg: colorHeaderFg, Bg: bg}
			}
		}
		// Cursor at end
		if cursor >= len(input) && inputX+len(input) < w-1 {
			cells[y*w+inputX+len(input)] = terminal.Cell{Rune: ' ', Fg: colorHeaderFg, Bg: colorCursorBg}
		}

		drawText(cells, w, 1, y+1, "Enter:save  Esc:cancel  ^A/^E:start/end  ^K:kill  ^U:clear", colorHelpFg, colorDefaultBg, terminal.AttrDim)
		return
	}

	if app.InputMode {
		inputStr := fmt.Sprintf("Filter [content]: %s_", app.InputBuffer)
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

// renderHelpBar draws the keybinding help bar
func (app *AppState) renderHelpBar(cells []terminal.Cell, w, y int) {
	help := "?:help Tab:pane [/]:cat j/k:nav Space:sel f:filter d:deps ^S:out ^Q:quit"
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

// getFileGroupSummary formats file's groups as "group(count)" string for current category
func getFileGroupSummary(fi *FileInfo, cat string) string {
	if fi == nil || cat == "" {
		return ""
	}

	catTags := fi.CategoryTags(cat)
	if catTags == nil || len(catTags) == 0 {
		return ""
	}

	// Collect groups sorted alphabetically
	groups := make([]string, 0, len(catTags))
	for g := range catTags {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var parts []string
	for _, g := range groups {
		mods := catTags[g]
		tagCount := 0
		for _, tags := range mods {
			tagCount += len(tags)
		}
		parts = append(parts, fmt.Sprintf("%s(%d)", g, tagCount))
	}

	return strings.Join(parts, " ")
}

// countFilesInModule counts files having any tag in specified group/module
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

// countFilesInGroup counts files having any tag in specified group
func (app *AppState) countFilesInGroup(cat, group string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if _, ok := fi.CategoryTags(cat)[group]; ok {
			count++
		}
	}
	return count
}

// countFilesWithTag counts files having specific tag
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