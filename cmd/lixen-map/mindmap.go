package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// MindmapSource indicates origin context of mindmap view
type MindmapSource int

const (
	MindmapSourcePackage MindmapSource = iota // From left pane - package/directory
	MindmapSourceTag                          // From right pane - tag/group
)

// MindmapState holds mindmap view navigation and display state
type MindmapState struct {
	Source      MindmapSource
	Title       string
	Items       []MindmapItem
	Cursor      int
	Scroll      int
	SourcePath  string // For package mode: directory path
	SourceGroup string // For tag mode: group name
	SourceTag   string // For tag mode: tag name (empty if group)
}

// MindmapItem represents a single row in mindmap file list
type MindmapItem struct {
	Depth    int
	IsDir    bool
	Path     string // File path for selection lookup, empty for dirs
	Name     string
	Package  string              // Package name for files
	DepCount int                 // Number of local imports
	Focus    map[string][]string // Focus tags
	Interact map[string][]string // Interact tags
}

// itemScreenHeight returns the number of screen rows an item occupies
func itemScreenHeight(item MindmapItem) int {
	if item.IsDir {
		return 1
	}
	return 3
}

// calcScreenOffset calculates screen row offset for item at given index
func calcScreenOffset(items []MindmapItem, index int) int {
	offset := 0
	for i := 0; i < index && i < len(items); i++ {
		offset += itemScreenHeight(items[i])
	}
	return offset
}

// findItemAtScreenRow finds item index containing the given screen row
func findItemAtScreenRow(items []MindmapItem, screenRow int) int {
	row := 0
	for i, item := range items {
		h := itemScreenHeight(item)
		if screenRow < row+h {
			return i
		}
		row += h
	}
	return len(items) - 1
}

// totalScreenRows calculates total screen rows needed for all items
func totalScreenRows(items []MindmapItem) int {
	total := 0
	for _, item := range items {
		total += itemScreenHeight(item)
	}
	return total
}

// EnterMindmap routes mindmap opening to the handler based on originating pane
func (app *AppState) EnterMindmap() {
	switch app.FocusPane {
	case PaneLeft:
		app.enterMindmapPackage()
	case PaneCenter:
		app.enterMindmapFocusTag()
	case PaneRight:
		app.enterMindmapInteractTag()
	}
}

// enterMindmapPackage opens mindmap for directory at tree cursor
func (app *AppState) enterMindmapPackage() {
	if len(app.TreeFlat) == 0 {
		app.Message = "no items to view"
		return
	}

	node := app.TreeFlat[app.TreeCursor]

	// If file, use parent directory
	targetNode := node
	if !node.IsDir && node.Parent != nil {
		targetNode = node.Parent
	}

	state := &MindmapState{
		Source:     MindmapSourcePackage,
		Title:      targetNode.Path,
		SourcePath: targetNode.Path,
	}

	// Build items from target node
	state.Items = app.buildPackageItems(targetNode, 0)

	if len(state.Items) == 0 {
		app.Message = "no files in directory"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

// enterMindmapFocusTag opens mindmap for focus tag or group at cursor
func (app *AppState) enterMindmapFocusTag() {
	if len(app.TagFlat) == 0 {
		app.Message = "no focus tags to view"
		return
	}

	item := app.TagFlat[app.TagCursor]

	state := &MindmapState{
		Source:      MindmapSourceTag,
		SourceGroup: item.Group,
		SourceTag:   item.Tag,
	}

	if item.IsGroup {
		state.Title = "#focus:" + item.Group
		state.Items = app.buildGroupItems(CategoryFocus, item.Group)
	} else {
		state.Title = fmt.Sprintf("#focus:%s{%s}", item.Group, item.Tag)
		state.Items = app.buildTagItems(CategoryFocus, item.Group, item.Tag)
	}

	if len(state.Items) == 0 {
		app.Message = "no files with this tag"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

// enterMindmapInteractTag opens mindmap for interact tag or group at cursor
func (app *AppState) enterMindmapInteractTag() {
	if len(app.InteractFlat) == 0 {
		app.Message = "no interact tags to view"
		return
	}

	item := app.InteractFlat[app.InteractCursor]

	state := &MindmapState{
		Source:      MindmapSourceTag,
		SourceGroup: item.Group,
		SourceTag:   item.Tag,
	}

	if item.IsGroup {
		state.Title = "#interact:" + item.Group
		state.Items = app.buildGroupItems(CategoryInteract, item.Group)
	} else {
		state.Title = fmt.Sprintf("#interact:%s{%s}", item.Group, item.Tag)
		state.Items = app.buildTagItems(CategoryInteract, item.Group, item.Tag)
	}

	if len(state.Items) == 0 {
		app.Message = "no files with this tag"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

// buildGroupItems creates mindmap items for all files in a tag group
func (app *AppState) buildGroupItems(cat Category, group string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	for _, path := range paths {
		fi := app.Index.Files[path]
		items = append(items, MindmapItem{
			Depth:    0,
			IsDir:    false,
			Path:     path,
			Name:     path,
			Package:  fi.Package,
			DepCount: len(fi.Imports),
			Focus:    fi.Focus,
			Interact: fi.Interact,
		})
	}

	return items
}

// buildTagItems creates mindmap items for files with specific tag
func (app *AppState) buildTagItems(cat Category, group, tag string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if tags, ok := fi.TagMap(cat)[group]; ok {
			for _, t := range tags {
				if t == tag {
					paths = append(paths, path)
					break
				}
			}
		}
	}
	sort.Strings(paths)

	for _, path := range paths {
		fi := app.Index.Files[path]
		items = append(items, MindmapItem{
			Depth:    0,
			IsDir:    false,
			Path:     path,
			Name:     path,
			Package:  fi.Package,
			DepCount: len(fi.Imports),
			Focus:    fi.Focus,
			Interact: fi.Interact,
		})
	}

	return items
}

// buildPackageItems constructs mindmap items from tree node hierarchy
func (app *AppState) buildPackageItems(node *TreeNode, depth int) []MindmapItem {
	var items []MindmapItem

	// Add directory header
	if node.IsDir {
		items = append(items, MindmapItem{
			Depth: depth,
			IsDir: true,
			Path:  "",
			Name:  node.Name + "/",
		})
	}

	// Sort children: dirs first, then files
	children := make([]*TreeNode, len(node.Children))
	copy(children, node.Children)
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return children[i].Name < children[j].Name
	})

	for _, child := range children {
		if child.IsDir {
			subItems := app.buildPackageItems(child, depth+1)
			items = append(items, subItems...)
		} else {
			var focus, interact map[string][]string
			var pkg string
			var depCount int
			if child.FileInfo != nil {
				focus = child.FileInfo.Focus
				interact = child.FileInfo.Interact
				pkg = child.FileInfo.Package
				depCount = len(child.FileInfo.Imports)
			}
			items = append(items, MindmapItem{
				Depth:    depth + 1,
				IsDir:    false,
				Path:     child.Path,
				Name:     child.Name,
				Package:  pkg,
				DepCount: depCount,
				Focus:    focus,
				Interact: interact,
			})
		}
	}

	return items
}

// HandleMindmapEvent processes keyboard input in mindmap view
func (app *AppState) HandleMindmapEvent(ev terminal.Event) {
	// Handle input mode first
	if app.InputMode {
		app.handleMindmapInputEvent(ev)
		return
	}

	state := app.MindmapState
	if state == nil {
		app.MindmapMode = false
		return
	}

	switch ev.Key {
	case terminal.KeyEscape:
		// Esc only clears filter/input, does not exit view
		if app.Filter.HasActiveFilter() {
			app.ClearFilter()
			app.Message = "filter cleared"
		}
		return

	case terminal.KeyRune:
		switch ev.Rune {
		case '?':
			app.HelpMode = true
			return
		case 'q':
			// Exit to pane view
			app.MindmapMode = false
			return
		case 'j':
			app.moveMindmapCursor(1)
		case 'k':
			app.moveMindmapCursor(-1)
		case ' ':
			app.toggleMindmapSelection()
		case 'a':
			app.selectAllMindmap()
		case 'c':
			app.clearMindmapSelection()
		case '0':
			app.jumpMindmapToStart()
		case '$':
			app.jumpMindmapToEnd()
		case 'f':
			app.applyMindmapFilter()
		case 'F':
			if app.Filter.HasActiveFilter() {
				count := app.selectFilteredFiles()
				app.Message = fmt.Sprintf("selected %d filtered files", count)
			} else {
				app.Message = "no filter active"
			}
		case '/':
			app.InputMode = true
			app.InputBuffer = ""
			app.SearchType = SearchTypeContent
		case 't':
			app.InputMode = true
			app.InputBuffer = ""
			app.SearchType = SearchTypeTags
		case 'g':
			app.InputMode = true
			app.InputBuffer = ""
			app.SearchType = SearchTypeGroups
		}

	case terminal.KeyEnter:
		app.EnterDive()
		return

	case terminal.KeyUp:
		app.moveMindmapCursor(-1)
	case terminal.KeyDown:
		app.moveMindmapCursor(1)
	case terminal.KeySpace:
		app.toggleMindmapSelection()
	case terminal.KeyPageUp:
		app.pageMindmapCursor(-1)
	case terminal.KeyPageDown:
		app.pageMindmapCursor(1)
	case terminal.KeyHome:
		app.jumpMindmapToStart()
	case terminal.KeyEnd:
		app.jumpMindmapToEnd()
	}
}

// handleMindmapInputEvent processes search input in mindmap view
func (app *AppState) handleMindmapInputEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.InputMode = false
		app.InputBuffer = ""

	case terminal.KeyEnter:
		app.InputMode = false
		app.executeSearch(app.InputBuffer)
		app.InputBuffer = ""

	case terminal.KeyBackspace:
		if len(app.InputBuffer) > 0 {
			app.InputBuffer = app.InputBuffer[:len(app.InputBuffer)-1]
		}

	case terminal.KeyRune:
		app.InputBuffer += string(ev.Rune)
	}
}

// applyMindmapFilter toggles filter for item at mindmap cursor
func (app *AppState) applyMindmapFilter() {
	state := app.MindmapState
	if state == nil || len(state.Items) == 0 {
		return
	}

	item := state.Items[state.Cursor]
	var paths []string

	if item.IsDir {
		// Collect all file paths under this directory item
		for i := state.Cursor + 1; i < len(state.Items); i++ {
			next := state.Items[i]
			if next.Depth <= item.Depth {
				break
			}
			if !next.IsDir && next.Path != "" {
				paths = append(paths, next.Path)
			}
		}
	} else if item.Path != "" {
		paths = []string{item.Path}
	}

	if len(paths) == 0 {
		return
	}

	// Check if already filtered - toggle behavior
	if app.isPathSetFiltered(paths) {
		app.RemoveFromFilter(paths)
		app.Message = fmt.Sprintf("unfilter: %s", item.Name)
	} else {
		app.ApplyFilter(paths)
		if item.IsDir {
			app.Message = fmt.Sprintf("filter: %s (%d files)", item.Name, len(paths))
		} else {
			app.Message = fmt.Sprintf("filter: %s", item.Name)
		}
	}
}

// moveMindmapCursor moves cursor with bounds and scroll adjustment
func (app *AppState) moveMindmapCursor(delta int) {
	state := app.MindmapState
	if state == nil || len(state.Items) == 0 {
		return
	}

	state.Cursor += delta
	if state.Cursor < 0 {
		state.Cursor = 0
	}
	if state.Cursor >= len(state.Items) {
		state.Cursor = len(state.Items) - 1
	}

	// Adjust scroll based on screen rows
	visibleRows := app.Height - 4 // Header + help
	if visibleRows < 1 {
		visibleRows = 1
	}

	cursorScreenStart := calcScreenOffset(state.Items, state.Cursor)
	cursorScreenEnd := cursorScreenStart + itemScreenHeight(state.Items[state.Cursor])

	if cursorScreenStart < state.Scroll {
		state.Scroll = cursorScreenStart
	}
	if cursorScreenEnd > state.Scroll+visibleRows {
		state.Scroll = cursorScreenEnd - visibleRows
	}
	if state.Scroll < 0 {
		state.Scroll = 0
	}
}

// pageMindmapCursor moves cursor by half-page increment
func (app *AppState) pageMindmapCursor(direction int) {
	visibleRows := app.Height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	// Move by roughly half-page worth of items
	delta := (visibleRows / 4) * direction // Divide by 4 since files take 3 rows
	if delta == 0 {
		delta = direction
	}
	app.moveMindmapCursor(delta)
}

// jumpMindmapToStart moves cursor to first mindmap item
func (app *AppState) jumpMindmapToStart() {
	if app.MindmapState == nil {
		return
	}
	app.MindmapState.Cursor = 0
	app.MindmapState.Scroll = 0
}

// jumpMindmapToEnd moves cursor to last mindmap item
func (app *AppState) jumpMindmapToEnd() {
	state := app.MindmapState
	if state == nil || len(state.Items) == 0 {
		return
	}
	state.Cursor = len(state.Items) - 1
	app.moveMindmapCursor(0)
}

// toggleMindmapSelection toggles selection of file at cursor
func (app *AppState) toggleMindmapSelection() {
	state := app.MindmapState
	if state == nil || len(state.Items) == 0 {
		return
	}

	item := state.Items[state.Cursor]
	if item.IsDir || item.Path == "" {
		return
	}

	if app.Selected[item.Path] {
		delete(app.Selected, item.Path)
	} else {
		app.Selected[item.Path] = true
	}
}

// selectAllMindmap selects all files visible in mindmap
func (app *AppState) selectAllMindmap() {
	state := app.MindmapState
	if state == nil {
		return
	}

	for _, item := range state.Items {
		if !item.IsDir && item.Path != "" {
			app.Selected[item.Path] = true
		}
	}
	app.Message = "selected all visible"
}

// clearMindmapSelection deselects all files visible in mindmap
func (app *AppState) clearMindmapSelection() {
	state := app.MindmapState
	if state == nil {
		return
	}

	for _, item := range state.Items {
		if !item.IsDir && item.Path != "" {
			delete(app.Selected, item.Path)
		}
	}
	app.Message = "cleared visible selections"
}

// formatTagsExpanded formats tags with spacing: #group{tag1, tag2} #group2{tag3}
func formatTagsExpanded(tagMap map[string][]string) string {
	if len(tagMap) == 0 {
		return ""
	}

	groups := make([]string, 0, len(tagMap))
	for g := range tagMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var parts []string
	for _, g := range groups {
		tags := make([]string, len(tagMap[g]))
		copy(tags, tagMap[g])
		sort.Strings(tags)
		parts = append(parts, fmt.Sprintf("#%s{%s}", g, strings.Join(tags, ", ")))
	}

	return strings.Join(parts, "  ")
}

// RenderMindmap draws mindmap view with file list and status
func (app *AppState) RenderMindmap(cells []terminal.Cell, w, h int) {
	state := app.MindmapState
	if state == nil {
		return
	}

	// Header
	drawRect(cells, 0, 0, w, 1, w, colorHeaderBg)
	title := fmt.Sprintf("MINDMAP: %s", state.Title)
	if len(title) > w-20 {
		title = title[:w-23] + "..."
	}
	drawText(cells, w, 1, 0, title, colorHeaderFg, colorHeaderBg, terminal.AttrBold)

	rightInfo := "[Space:sel] [q:back]"
	drawText(cells, w, w-len(rightInfo)-1, 0, rightInfo, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	// Get expanded files for dependency highlighting
	depExpanded := make(map[string]bool)
	if app.ExpandDeps && len(app.Selected) > 0 {
		depExpanded = app.computeDepExpandedFiles()
	}

	// Check if filter is active
	hasFilter := app.Filter.HasActiveFilter()

	// Content area
	contentTop := 1
	contentHeight := h - 2 // Header + help

	// Render items based on screen row position
	screenRow := 0
	for idx, item := range state.Items {
		itemH := itemScreenHeight(item)

		// Skip items before scroll position
		if screenRow+itemH <= state.Scroll {
			screenRow += itemH
			continue
		}

		// Stop if we're past visible area
		if screenRow >= state.Scroll+contentHeight {
			break
		}

		// Calculate visible portion of this item
		itemStartY := contentTop + (screenRow - state.Scroll)

		// Check filter match
		matchesFilter := true
		if hasFilter && !item.IsDir && item.Path != "" {
			matchesFilter = app.Filter.FilteredPaths[item.Path]
		}
		dimmed := hasFilter && !matchesFilter

		isCursor := idx == state.Cursor
		rowBg := colorDefaultBg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Indentation
		indent := item.Depth * 2

		if item.IsDir {
			// Directory: single line
			y := itemStartY
			if y >= contentTop && y < contentTop+contentHeight {
				// Clear line
				for x := 0; x < w; x++ {
					cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
				}

				x := 1 + indent
				drawText(cells, w, x, y, item.Name, colorDirFg, rowBg, terminal.AttrBold)
			}
		} else {
			// File: 3 lines
			isSelected := app.Selected[item.Path]
			isDepExpanded := depExpanded[item.Path]

			// Line 1: checkbox + filename + package + deps
			y1 := itemStartY
			if y1 >= contentTop && y1 < contentTop+contentHeight {
				for x := 0; x < w; x++ {
					cells[y1*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
				}

				x := 1 + indent

				// Checkbox
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
				drawText(cells, w, x, y1, checkbox, checkFg, rowBg, terminal.AttrNone)
				x += 4

				// Filename
				fi := app.Index.Files[item.Path]
				nameFg := colorDefaultFg
				if fi != nil && fi.IsAll {
					nameFg = colorAllTagFg
				}
				if dimmed {
					nameFg = colorUnselected
				}

				name := item.Name
				maxNameLen := 35
				if len(name) > maxNameLen {
					name = "..." + name[len(name)-maxNameLen+3:]
				}
				attr := terminal.AttrNone
				if dimmed {
					attr = terminal.AttrDim
				}
				drawText(cells, w, x, y1, name, nameFg, rowBg, attr)
				x += len(name) + 2

				// Package and deps info
				infoStr := fmt.Sprintf("pkg:%s  deps:%d", item.Package, item.DepCount)
				infoFg := colorStatusFg
				if dimmed {
					infoFg = colorUnselected
				}
				drawText(cells, w, x, y1, infoStr, infoFg, rowBg, terminal.AttrDim)
			}

			// Line 2: Focus tags
			y2 := itemStartY + 1
			if y2 >= contentTop && y2 < contentTop+contentHeight {
				for x := 0; x < w; x++ {
					cells[y2*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
				}

				x := 1 + indent + 4 // Align with filename
				label := "Focus: "
				labelFg := colorExpandedFg
				if dimmed {
					labelFg = colorUnselected
				}
				drawText(cells, w, x, y2, label, labelFg, rowBg, terminal.AttrNone)
				x += len(label)

				if len(item.Focus) > 0 {
					focusStr := formatTagsExpanded(item.Focus)
					maxLen := w - x - 2
					if len(focusStr) > maxLen && maxLen > 3 {
						focusStr = focusStr[:maxLen-1] + "…"
					}
					if dimmed {
						drawText(cells, w, x, y2, focusStr, colorUnselected, rowBg, terminal.AttrDim)
					} else {
						drawColoredTags(cells, w, x, y2, focusStr, rowBg)
					}
				} else {
					drawText(cells, w, x, y2, "(none)", colorUnselected, rowBg, terminal.AttrDim)
				}
			}

			// Line 3: Interact tags
			y3 := itemStartY + 2
			if y3 >= contentTop && y3 < contentTop+contentHeight {
				for x := 0; x < w; x++ {
					cells[y3*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
				}

				x := 1 + indent + 4 // Align with filename
				label := "Interact: "
				labelFg := colorExpandedFg
				if dimmed {
					labelFg = colorUnselected
				}
				drawText(cells, w, x, y3, label, labelFg, rowBg, terminal.AttrNone)
				x += len(label)

				if len(item.Interact) > 0 {
					interactStr := formatTagsExpanded(item.Interact)
					maxLen := w - x - 2
					if len(interactStr) > maxLen && maxLen > 3 {
						interactStr = interactStr[:maxLen-1] + "…"
					}
					if dimmed {
						drawText(cells, w, x, y3, interactStr, colorUnselected, rowBg, terminal.AttrDim)
					} else {
						drawColoredTags(cells, w, x, y3, interactStr, rowBg)
					}
				} else {
					drawText(cells, w, x, y3, "(none)", colorUnselected, rowBg, terminal.AttrDim)
				}
			}
		}

		screenRow += itemH
	}

	// Input mode overlay
	if app.InputMode {
		typeHint := "content"
		switch app.SearchType {
		case SearchTypeTags:
			typeHint = "tags"
		case SearchTypeGroups:
			typeHint = "groups"
		}
		inputStr := fmt.Sprintf("Search [%s]: %s_", typeHint, app.InputBuffer)
		drawText(cells, w, 1, h-2, inputStr, colorHeaderFg, colorInputBg, terminal.AttrNone)
	}

	// Help bar
	helpY := h - 1
	help := "?:help j/k:nav Space:sel f:filter /:search t:tag g:group Enter:dive q:back ^Q:quit"
	drawText(cells, w, 1, helpY, help, colorHelpFg, colorDefaultBg, terminal.AttrDim)

	// Scroll indicator
	totalRows := totalScreenRows(state.Items)
	if totalRows > contentHeight {
		pct := 0
		if totalRows > 0 {
			pct = (state.Scroll * 100) / totalRows
		}
		scrollStr := fmt.Sprintf("[%d%%]", pct)
		drawText(cells, w, w-len(scrollStr)-1, helpY, scrollStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
	}
}