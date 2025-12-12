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
	Depth  int
	IsDir  bool
	Path   string // File path for selection lookup, empty for dirs
	Name   string
	TagStr string // Formatted tags
}

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

// formatDirTags aggregates tags from immediate children of directory
func (app *AppState) formatDirTags(node *TreeNode) string {
	focusGroups := make(map[string]map[string]bool)
	interactGroups := make(map[string]map[string]bool)

	for _, child := range node.Children {
		if child.IsDir || child.FileInfo == nil {
			continue
		}
		// Aggregate focus
		for group, tags := range child.FileInfo.Focus {
			if focusGroups[group] == nil {
				focusGroups[group] = make(map[string]bool)
			}
			for _, t := range tags {
				focusGroups[group][t] = true
			}
		}
		// Aggregate interact
		for group, tags := range child.FileInfo.Interact {
			if interactGroups[group] == nil {
				interactGroups[group] = make(map[string]bool)
			}
			for _, t := range tags {
				interactGroups[group][t] = true
			}
		}
	}

	var parts []string
	if len(focusGroups) > 0 {
		parts = append(parts, formatTagGroups("focus", focusGroups))
	}
	if len(interactGroups) > 0 {
		parts = append(parts, formatTagGroups("interact", interactGroups))
	}

	return strings.Join(parts, " ")
}

// formatFileTags formats single file's tags as display string (both Focus and Interact)
func formatFileTags(fi *FileInfo) string {
	if fi == nil {
		return ""
	}

	var parts []string

	// Focus tags
	if len(fi.Focus) > 0 {
		parts = append(parts, formatTagMap("focus", fi.Focus))
	}

	// Interact tags
	if len(fi.Interact) > 0 {
		parts = append(parts, formatTagMap("interact", fi.Interact))
	}

	return strings.Join(parts, " ")
}

// formatTagMap formats a tag map as #block{group[tags],...} string
func formatTagMap(block string, tagMap map[string][]string) string {
	if len(tagMap) == 0 {
		return ""
	}

	groups := make([]string, 0, len(tagMap))
	for g := range tagMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var groupParts []string
	for _, g := range groups {
		tags := make([]string, len(tagMap[g]))
		copy(tags, tagMap[g])
		sort.Strings(tags)
		groupParts = append(groupParts, fmt.Sprintf("%s[%s]", g, strings.Join(tags, ",")))
	}

	return fmt.Sprintf("#%s{%s}", block, strings.Join(groupParts, ","))
}

// formatTagGroups formats tag set map as #block{group[tags],...} string
func formatTagGroups(block string, tagGroups map[string]map[string]bool) string {
	if len(tagGroups) == 0 {
		return ""
	}

	groups := make([]string, 0, len(tagGroups))
	for g := range tagGroups {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var groupParts []string
	for _, g := range groups {
		tags := make([]string, 0, len(tagGroups[g]))
		for t := range tagGroups[g] {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		groupParts = append(groupParts, fmt.Sprintf("%s[%s]", g, strings.Join(tags, ",")))
	}

	return fmt.Sprintf("#%s{%s}", block, strings.Join(groupParts, ","))
}

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
		state.Items = app.buildGroupItems(item.Group)
	} else {
		state.Title = fmt.Sprintf("#focus:%s{%s}", item.Group, item.Tag)
		state.Items = app.buildTagItems(item.Group, item.Tag)
	}

	if len(state.Items) == 0 {
		app.Message = "no files with this tag"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

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
		state.Items = app.buildInteractGroupItems(item.Group)
	} else {
		state.Title = fmt.Sprintf("#interact:%s{%s}", item.Group, item.Tag)
		state.Items = app.buildInteractTagItems(item.Group, item.Tag)
	}

	if len(state.Items) == 0 {
		app.Message = "no files with this tag"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

func (app *AppState) buildInteractGroupItems(group string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if _, ok := fi.Interact[group]; ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	for _, path := range paths {
		fi := app.Index.Files[path]
		items = append(items, MindmapItem{
			Depth:  0,
			IsDir:  false,
			Path:   path,
			Name:   path,
			TagStr: formatFileTags(fi),
		})
	}

	return items
}

func (app *AppState) buildInteractTagItems(group, tag string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if tags, ok := fi.Interact[group]; ok {
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
			Depth:  0,
			IsDir:  false,
			Path:   path,
			Name:   path,
			TagStr: formatFileTags(fi),
		})
	}

	return items
}

// buildPackageItems constructs mindmap items from tree node hierarchy
func (app *AppState) buildPackageItems(node *TreeNode, depth int) []MindmapItem {
	var items []MindmapItem

	// Add directory header
	if node.IsDir {
		tagStr := app.formatDirTags(node)
		items = append(items, MindmapItem{
			Depth:  depth,
			IsDir:  true,
			Path:   "",
			Name:   node.Name + "/",
			TagStr: tagStr,
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
			tagStr := ""
			if child.FileInfo != nil {
				tagStr = formatFileTags(child.FileInfo)
			}
			items = append(items, MindmapItem{
				Depth:  depth + 1,
				IsDir:  false,
				Path:   child.Path,
				Name:   child.Name,
				TagStr: tagStr,
			})
		}
	}

	return items
}

// buildGroupItems creates mindmap items for all files in a focus group
func (app *AppState) buildGroupItems(group string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if _, ok := fi.Focus[group]; ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)

	for _, path := range paths {
		fi := app.Index.Files[path]
		items = append(items, MindmapItem{
			Depth:  0,
			IsDir:  false,
			Path:   path,
			Name:   path,
			TagStr: formatFileTags(fi),
		})
	}

	return items
}

// buildTagItems creates mindmap items for files with specific focus tag
func (app *AppState) buildTagItems(group, tag string) []MindmapItem {
	var items []MindmapItem

	var paths []string
	for path, fi := range app.Index.Files {
		if tags, ok := fi.Focus[group]; ok {
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
			Depth:  0,
			IsDir:  false,
			Path:   path,
			Name:   path,
			TagStr: formatFileTags(fi),
		})
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
		case 'q':
			// Exit to 2-pane view
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

	// Adjust scroll
	visibleRows := app.Height - 4 // Header + help
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

// pageMindmapCursor moves cursor by half-page increment
func (app *AppState) pageMindmapCursor(direction int) {
	visibleRows := app.Height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}
	delta := (visibleRows / 2) * direction
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

	for i := 0; i < contentHeight && state.Scroll+i < len(state.Items); i++ {
		y := contentTop + i
		idx := state.Scroll + i
		item := state.Items[idx]

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

		// Clear line
		for x := 0; x < w; x++ {
			cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
		}

		// Indentation
		indent := item.Depth * 2
		x := 1 + indent

		if item.IsDir {
			// Directory line
			drawText(cells, w, x, y, item.Name, colorDirFg, rowBg, terminal.AttrBold)
			x += len(item.Name) + 2

			// Tags for directory (colored)
			if item.TagStr != "" && x < w-2 {
				drawColoredTags(cells, w, x, y, item.TagStr, rowBg)
			}
		} else {
			// File line - show checkbox
			isSelected := app.Selected[item.Path]
			isDepExpanded := depExpanded[item.Path]

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

			drawText(cells, w, x, y, checkbox, checkFg, rowBg, terminal.AttrNone)
			x += 4

			// File name
			fi := app.Index.Files[item.Path]
			nameFg := colorDefaultFg
			if fi != nil && fi.IsAll {
				nameFg = colorAllTagFg
			}

			name := item.Name
			maxNameLen := 40
			if len(name) > maxNameLen {
				name = "..." + name[len(name)-maxNameLen+3:]
			}
			attr := terminal.AttrNone
			if dimmed {
				attr = terminal.AttrDim
			}
			drawText(cells, w, x, y, name, nameFg, rowBg, attr)
			x += len(name) + 2

			// Tags (colored)
			if item.TagStr != "" && x < w-2 {
				drawColoredTags(cells, w, x, y, item.TagStr, rowBg)
			}
		}
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
	help := "j/k:nav 0/$:jump Space:toggle a:all c:clear f:filter F:sel-filter /:search t:tag g:group q:back"
	drawText(cells, w, 1, helpY, help, colorHelpFg, colorDefaultBg, terminal.AttrDim)

	// Scroll indicator
	if len(state.Items) > contentHeight {
		pct := 0
		if len(state.Items) > 0 {
			pct = (state.Scroll * 100) / len(state.Items)
		}
		scrollStr := fmt.Sprintf("[%d%%]", pct)
		drawText(cells, w, w-len(scrollStr)-1, helpY, scrollStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
	}
}