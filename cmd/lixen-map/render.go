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
	} else if app.PreviewMode {
		app.renderPreview(cells, w, h)
	} else {
		app.renderSplitPane(cells, w, h)
	}

	app.Term.Flush(cells, w, h)
}

// renderSplitPane draws the three-pane main view layout
func (app *AppState) renderSplitPane(cells []terminal.Cell, w, h int) {
	// Calculate pane widths - equal thirds
	paneWidth := (w - 2) / 3 // -2 for two borders
	leftWidth := paneWidth
	centerWidth := paneWidth
	rightWidth := w - leftWidth - centerWidth - 2

	// Header
	drawRect(cells, 0, 0, w, headerHeight, w, colorHeaderBg)
	title := "LIXEN-MAP"
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	// Compute output stats once
	totalFiles, depFiles, totalSize, depSize := app.computeOutputStats()

	// Determine size color based on threshold
	sizeFg := colorHeaderFg
	if totalSize > SizeWarningThreshold {
		sizeFg = terminal.RGB{R: 255, G: 80, B: 80}
	}

	// Right side header info - draw segments with different colors
	// Format: "Deps: N | Output: N files | Size: X (+Y dep)"
	x := w - 2

	// Build from right to left
	if app.ExpandDeps && depSize > 0 {
		depSizeStr := fmt.Sprintf("(+%s dep)", formatSize(depSize))
		x -= len(depSizeStr)
		drawText(cells, w, x, 0, depSizeStr, colorStatusFg, colorHeaderBg, terminal.AttrNone)
		x -= 1 // space
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
		depsVal = fmt.Sprintf("%d", app.DepthLimit)
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

	// Pane backgrounds
	leftBg := colorDefaultBg
	if app.FocusPane == PaneLeft {
		leftBg = colorPaneActiveBg
	}
	drawRect(cells, 0, contentTop, leftWidth, contentHeight, w, leftBg)

	centerBg := colorDefaultBg
	if app.FocusPane == PaneCenter {
		centerBg = colorPaneActiveBg
	}
	centerStart := leftWidth + 1
	drawRect(cells, centerStart, contentTop, centerWidth, contentHeight, w, centerBg)

	rightBg := colorDefaultBg
	if app.FocusPane == PaneRight {
		rightBg = colorPaneActiveBg
	}
	rightStart := leftWidth + centerWidth + 2
	drawRect(cells, rightStart, contentTop, rightWidth, contentHeight, w, rightBg)

	// Vertical borders
	for y := contentTop; y < contentTop+contentHeight; y++ {
		cells[y*w+leftWidth] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		cells[y*w+leftWidth+centerWidth+1] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	hasFilter := app.Filter.HasActiveFilter()

	// Pane headers with counters
	leftSel, leftFil, leftTotal := app.computeTreePaneCounts()
	centerSelG, centerTotalG, centerSelT, centerTotalT, centerFilG, centerFilT := app.computeFocusPaneCounts()
	rightSelG, rightTotalG, rightSelT, rightTotalT, rightFilG, rightFilT := app.computeInteractPaneCounts()

	leftHeader := "PACKAGES / FILES"
	centerHeader := "FOCUS GROUPS"
	rightHeader := "INTERACT GROUPS"

	drawText(cells, w, 1, contentTop, leftHeader, colorStatusFg, leftBg, terminal.AttrBold)
	drawText(cells, w, centerStart+1, contentTop, centerHeader, colorStatusFg, centerBg, terminal.AttrBold)
	drawText(cells, w, rightStart+1, contentTop, rightHeader, colorStatusFg, rightBg, terminal.AttrBold)

	// Counters on same line, right-aligned within pane
	leftCounter := formatTreePaneCounter(leftSel, leftFil, leftTotal, depFiles, app.ExpandDeps, hasFilter)
	centerCounter := formatTagPaneCounter(centerSelG, centerTotalG, centerSelT, centerTotalT, centerFilG, centerFilT, hasFilter)
	rightCounter := formatTagPaneCounter(rightSelG, rightTotalG, rightSelT, rightTotalT, rightFilG, rightFilT, hasFilter)

	leftCounterX := leftWidth - len(leftCounter) - 1
	if leftCounterX > len(leftHeader)+2 {
		drawTreePaneCounter(cells, w, leftCounterX, contentTop, leftCounter, leftSel, leftFil, leftTotal, depFiles, app.ExpandDeps, hasFilter, leftBg)
	}

	centerCounterX := centerStart + centerWidth - len(centerCounter) - 1
	if centerCounterX > centerStart+len(centerHeader)+2 {
		drawText(cells, w, centerCounterX, contentTop, centerCounter, colorMatchCountFg, centerBg, terminal.AttrNone)
	}

	rightCounterX := rightStart + rightWidth - len(rightCounter) - 1
	if rightCounterX > rightStart+len(rightHeader)+2 {
		drawText(cells, w, rightCounterX, contentTop, rightCounter, colorMatchCountFg, rightBg, terminal.AttrNone)
	}

	// Render pane contents
	paneContentTop := contentTop + 1
	paneContentHeight := contentHeight - 1

	app.renderTreePane(cells, w, 0, leftWidth, paneContentTop, paneContentHeight)
	app.renderFocusPane(cells, w, centerStart, centerWidth, paneContentTop, paneContentHeight)
	app.renderInteractPane(cells, w, rightStart, rightWidth, paneContentTop, paneContentHeight)

	// Status area
	statusY := h - statusHeight - helpHeight
	app.renderStatus(cells, w, statusY)

	// Help bar
	helpY := h - helpHeight
	app.renderHelp(cells, w, helpY)
}

// renderTreePane draws the tree pane with files and directories
func (app *AppState) renderTreePane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
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

// renderFocusPane draws the focus tag pane (center) with 3-level support
func (app *AppState) renderFocusPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneCenter {
		bg = colorPaneActiveBg
	}

	hasFilter := app.Filter.HasActiveFilter()

	for i := 0; i < height && app.TagScroll+i < len(app.TagFlat); i++ {
		y := startY + i
		idx := app.TagScroll + i
		item := app.TagFlat[idx]

		isCursor := idx == app.TagCursor && app.FocusPane == PaneCenter
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
			isFiltered := app.isGroupFiltered(CategoryFocus, item.Group)
			dimmed := hasFilter && !isFiltered
			selState := app.computeGroupSelectionState(CategoryFocus, item.Group)

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

			fileCount := app.countFilesInGroup(CategoryFocus, item.Group)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeModule:
			isFiltered := app.isModuleFiltered(CategoryFocus, item.Group, item.Module)
			dimmed := hasFilter && !isFiltered
			selState := app.computeModuleSelectionState(CategoryFocus, item.Group, item.Module)

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

			fileCount := app.countFilesInModule(CategoryFocus, item.Group, item.Module)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeTag:
			isFiltered := app.isTagFiltered(CategoryFocus, item.Group, item.Module, item.Tag)
			dimmed := hasFilter && !isFiltered
			selState := app.computeTagSelectionState(CategoryFocus, item.Group, item.Module, item.Tag)

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

			fileCount := app.countFilesWithTag(CategoryFocus, item.Group, item.Module, item.Tag)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)
		}
	}
}

// renderInteractPane draws the interact tag pane (right) with 3-level support
func (app *AppState) renderInteractPane(cells []terminal.Cell, totalWidth, startX, paneWidth, startY, height int) {
	bg := colorDefaultBg
	if app.FocusPane == PaneRight {
		bg = colorPaneActiveBg
	}

	hasFilter := app.Filter.HasActiveFilter()

	for i := 0; i < height && app.InteractScroll+i < len(app.InteractFlat); i++ {
		y := startY + i
		idx := app.InteractScroll + i
		item := app.InteractFlat[idx]

		isCursor := idx == app.InteractCursor && app.FocusPane == PaneRight
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
			isFiltered := app.isGroupFiltered(CategoryInteract, item.Group)
			dimmed := hasFilter && !isFiltered
			selState := app.computeGroupSelectionState(CategoryInteract, item.Group)

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

			fileCount := app.countFilesInGroup(CategoryInteract, item.Group)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeModule:
			isFiltered := app.isModuleFiltered(CategoryInteract, item.Group, item.Module)
			dimmed := hasFilter && !isFiltered
			selState := app.computeModuleSelectionState(CategoryInteract, item.Group, item.Module)

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

			fileCount := app.countFilesInModule(CategoryInteract, item.Group, item.Module)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)

		case TagItemTypeTag:
			isFiltered := app.isTagFiltered(CategoryInteract, item.Group, item.Module, item.Tag)
			dimmed := hasFilter && !isFiltered
			selState := app.computeTagSelectionState(CategoryInteract, item.Group, item.Module, item.Tag)

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

			fileCount := app.countFilesWithTag(CategoryInteract, item.Group, item.Module, item.Tag)
			countStr := fmt.Sprintf(" (%d)", fileCount)
			countFg := colorStatusFg
			if dimmed {
				countFg = colorUnselected
			}
			drawText(cells, totalWidth, x, y, countStr, countFg, rowBg, terminal.AttrNone)
		}
	}
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

// renderHelp draws the keybinding help bar
func (app *AppState) renderHelp(cells []terminal.Cell, w, y int) {
	help := "?:help Tab:pane j/k:nav Space:sel f:filter m:mode d:deps Enter:view ^S:out ^Q:quit"
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
func (app *AppState) getTagDirectories(group, module, tag string) []string {
	dirSet := make(map[string]bool)

	for path, fi := range app.Index.Files {
		if mods, ok := fi.Focus[group]; ok {
			if tags, ok := mods[module]; ok {
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

// computeTreePaneCounts returns selected, filtered, total counts for a pane
func (app *AppState) computeTreePaneCounts() (sel, fil, total int) {
	for path := range app.Index.Files {
		total++
		if app.Selected[path] {
			sel++
		}
		if app.Filter.FilteredPaths[path] {
			fil++
		}
	}
	return
}

// computeFocusPaneCounts returns group/tag selection and filter counts
func (app *AppState) computeFocusPaneCounts() (selGrps, totalGrps, selTags, totalTags, filGrps, filTags int) {
	totalGrps = len(app.Index.FocusGroups)

	for _, group := range app.Index.FocusGroups {
		if app.computeGroupSelectionState(CategoryFocus, group) != TagSelectNone {
			selGrps++
		}
		if app.isGroupFiltered(CategoryFocus, group) {
			filGrps++
		}

		// Count tags across all modules
		if modTags, ok := app.Index.FocusTags[group]; ok {
			for module, tags := range modTags {
				for _, tag := range tags {
					totalTags++
					if app.computeTagSelectionState(CategoryFocus, group, module, tag) != TagSelectNone {
						selTags++
					}
					if app.isTagFiltered(CategoryFocus, group, module, tag) {
						filTags++
					}
				}
			}
		}
	}
	return
}

// computeInteractPaneCounts returns group/tag selection and filter counts
func (app *AppState) computeInteractPaneCounts() (selGrps, totalGrps, selTags, totalTags, filGrps, filTags int) {
	totalGrps = len(app.Index.InteractGroups)

	for _, group := range app.Index.InteractGroups {
		if app.computeGroupSelectionState(CategoryInteract, group) != TagSelectNone {
			selGrps++
		}
		if app.isGroupFiltered(CategoryInteract, group) {
			filGrps++
		}

		// Count tags across all modules
		if modTags, ok := app.Index.InteractTags[group]; ok {
			for module, tags := range modTags {
				for _, tag := range tags {
					totalTags++
					if app.computeTagSelectionState(CategoryInteract, group, module, tag) != TagSelectNone {
						selTags++
					}
					if app.isTagFiltered(CategoryInteract, group, module, tag) {
						filTags++
					}
				}
			}
		}
	}
	return
}

// formatTagPaneCounter formats counter for Focus/Interact panes showing group/tag counts
func formatTagPaneCounter(selGrps, totalGrps, selTags, totalTags, filGrps, filTags int, hasFilter bool) string {
	if hasFilter {
		return fmt.Sprintf("[%d/%dg %d/%dt|%dg%dt]", selGrps, totalGrps, selTags, totalTags, filGrps, filTags)
	}
	return fmt.Sprintf("[%d/%dg %d/%dt]", selGrps, totalGrps, selTags, totalTags)
}

// formatTreePaneCounter formats counter string for tree pane header with deps
func formatTreePaneCounter(sel, fil, total, deps int, depsEnabled, hasFilter bool) string {
	if hasFilter {
		return fmt.Sprintf("[Sel: %d/%d | Fil: %d | Deps: %d]", sel, total, fil, deps)
	}
	return fmt.Sprintf("[Sel: %d/%d | Deps: %d]", sel, total, deps)
}

// drawTreePaneCounter renders tree pane counter with conditional dim for deps
func drawTreePaneCounter(cells []terminal.Cell, w, x, y int, counter string, sel, fil, total, deps int, depsEnabled, hasFilter bool, bg terminal.RGB) {
	// Find position of "Deps:" in counter to apply conditional styling
	depsIdx := len(counter) - len(fmt.Sprintf("Deps: %d]", deps))

	// Draw everything before deps section
	for i, r := range counter[:depsIdx] {
		cells[y*w+x+i] = terminal.Cell{Rune: r, Fg: colorMatchCountFg, Bg: bg}
	}

	// Draw deps section with conditional dim
	depsAttr := terminal.AttrNone
	depsFg := colorMatchCountFg
	if !depsEnabled {
		depsAttr = terminal.AttrDim
		depsFg = colorUnselected
	}

	depsSection := counter[depsIdx:]
	for i, r := range depsSection {
		cells[y*w+x+depsIdx+i] = terminal.Cell{Rune: r, Fg: depsFg, Bg: bg, Attrs: depsAttr}
	}
}

// RefreshFocusFlat rebuilds flattened tag list from index with 3-level support
func (app *AppState) RefreshFocusFlat() {
	app.TagFlat = nil

	for _, group := range app.Index.FocusGroups {
		groupExpanded := true
		if exp, ok := app.GroupExpanded[group]; ok {
			groupExpanded = exp
		} else {
			app.GroupExpanded[group] = true
		}

		app.TagFlat = append(app.TagFlat, TagItem{
			Type:     TagItemTypeGroup,
			Group:    group,
			Expanded: groupExpanded,
		})

		if !groupExpanded {
			continue
		}

		// 2-level direct tags (DirectTagsModule)
		if tags, ok := app.Index.FocusTags[group][DirectTagsModule]; ok {
			for _, tag := range tags {
				app.TagFlat = append(app.TagFlat, TagItem{
					Type:   TagItemTypeTag,
					Group:  group,
					Module: DirectTagsModule,
					Tag:    tag,
				})
			}
		}

		// 3-level modules
		if modules, ok := app.Index.FocusModules[group]; ok {
			for _, module := range modules {
				moduleKey := group + "." + module
				moduleExpanded := true
				if exp, ok := app.ModuleExpanded[moduleKey]; ok {
					moduleExpanded = exp
				} else {
					if app.ModuleExpanded == nil {
						app.ModuleExpanded = make(map[string]bool)
					}
					app.ModuleExpanded[moduleKey] = true
				}

				app.TagFlat = append(app.TagFlat, TagItem{
					Type:     TagItemTypeModule,
					Group:    group,
					Module:   module,
					Expanded: moduleExpanded,
				})

				if !moduleExpanded {
					continue
				}

				if tags, ok := app.Index.FocusTags[group][module]; ok {
					for _, tag := range tags {
						app.TagFlat = append(app.TagFlat, TagItem{
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

	if app.TagCursor >= len(app.TagFlat) {
		app.TagCursor = len(app.TagFlat) - 1
	}
	if app.TagCursor < 0 {
		app.TagCursor = 0
	}
}

// RefreshInteractFlat rebuilds flattened interact tag list from index with 3-level support
func (app *AppState) RefreshInteractFlat() {
	app.InteractFlat = nil

	for _, group := range app.Index.InteractGroups {
		groupExpanded := true
		if exp, ok := app.InteractGroupExpanded[group]; ok {
			groupExpanded = exp
		} else {
			app.InteractGroupExpanded[group] = true
		}

		app.InteractFlat = append(app.InteractFlat, TagItem{
			Type:     TagItemTypeGroup,
			Group:    group,
			Expanded: groupExpanded,
		})

		if !groupExpanded {
			continue
		}

		// 2-level direct tags (DirectTagsModule)
		if tags, ok := app.Index.InteractTags[group][DirectTagsModule]; ok {
			for _, tag := range tags {
				app.InteractFlat = append(app.InteractFlat, TagItem{
					Type:   TagItemTypeTag,
					Group:  group,
					Module: DirectTagsModule,
					Tag:    tag,
				})
			}
		}

		// 3-level modules
		if modules, ok := app.Index.InteractModules[group]; ok {
			for _, module := range modules {
				moduleKey := group + "." + module
				moduleExpanded := true
				if exp, ok := app.ModuleInteractExpanded[moduleKey]; ok {
					moduleExpanded = exp
				} else {
					if app.ModuleInteractExpanded == nil {
						app.ModuleInteractExpanded = make(map[string]bool)
					}
					app.ModuleInteractExpanded[moduleKey] = true
				}

				app.InteractFlat = append(app.InteractFlat, TagItem{
					Type:     TagItemTypeModule,
					Group:    group,
					Module:   module,
					Expanded: moduleExpanded,
				})

				if !moduleExpanded {
					continue
				}

				if tags, ok := app.Index.InteractTags[group][module]; ok {
					for _, tag := range tags {
						app.InteractFlat = append(app.InteractFlat, TagItem{
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

	if app.InteractCursor >= len(app.InteractFlat) {
		app.InteractCursor = len(app.InteractFlat) - 1
	}
	if app.InteractCursor < 0 {
		app.InteractCursor = 0
	}
}

// countFilesInModule counts files having any tag in specified group/module
func (app *AppState) countFilesInModule(cat Category, group, module string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if mods, ok := fi.TagMap(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				count++
			}
		}
	}
	return count
}

// countFilesInGroup counts files having any tag in specified group
func (app *AppState) countFilesInGroup(cat Category, group string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
			count++
		}
	}
	return count
}

// countFilesWithTag counts files having specific tag
func (app *AppState) countFilesWithTag(cat Category, group, module, tag string) int {
	count := 0
	for _, fi := range app.Index.Files {
		if mods, ok := fi.TagMap(cat)[group]; ok {
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