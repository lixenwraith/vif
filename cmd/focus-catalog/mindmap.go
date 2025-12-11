package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// MindmapSource indicates what triggered the mindmap
type MindmapSource int

const (
	MindmapSourcePackage MindmapSource = iota // From left pane - package/directory
	MindmapSourceTag                          // From right pane - tag/group
)

// MindmapState holds mindmap view state
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

// MindmapItem represents a single item in mindmap view
type MindmapItem struct {
	Depth  int
	IsDir  bool
	Path   string // File path for selection lookup, empty for dirs
	Name   string
	TagStr string // Formatted tags
}

// EnterMindmap switches to mindmap view based on current pane context
func (app *AppState) EnterMindmap() {
	if app.FocusPane == PaneLeft {
		app.enterMindmapPackage()
	} else {
		app.enterMindmapTag()
	}
}

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
			// Recurse into subdirectories
			subItems := app.buildPackageItems(child, depth+1)
			items = append(items, subItems...)
		} else {
			// File item
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

func (app *AppState) formatDirTags(node *TreeNode) string {
	// Aggregate all tags from files in this directory (non-recursive)
	tagGroups := make(map[string]map[string]bool)

	for _, child := range node.Children {
		if child.IsDir || child.FileInfo == nil {
			continue
		}
		for group, tags := range child.FileInfo.Tags {
			if tagGroups[group] == nil {
				tagGroups[group] = make(map[string]bool)
			}
			for _, t := range tags {
				tagGroups[group][t] = true
			}
		}
	}

	return formatTagGroups(tagGroups)
}

func formatFileTags(fi *FileInfo) string {
	tagGroups := make(map[string]map[string]bool)
	for group, tags := range fi.Tags {
		tagGroups[group] = make(map[string]bool)
		for _, t := range tags {
			tagGroups[group][t] = true
		}
	}
	return formatTagGroups(tagGroups)
}

func formatTagGroups(tagGroups map[string]map[string]bool) string {
	if len(tagGroups) == 0 {
		return ""
	}

	// Sort groups for consistent output
	groups := make([]string, 0, len(tagGroups))
	for g := range tagGroups {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	var parts []string
	for _, group := range groups {
		tags := make([]string, 0, len(tagGroups[group]))
		for t := range tagGroups[group] {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		parts = append(parts, fmt.Sprintf("#%s{%s}", group, strings.Join(tags, ",")))
	}

	return strings.Join(parts, " ")
}

func (app *AppState) enterMindmapTag() {
	if len(app.TagFlat) == 0 {
		app.Message = "no tags to view"
		return
	}

	item := app.TagFlat[app.TagCursor]

	state := &MindmapState{
		Source:      MindmapSourceTag,
		SourceGroup: item.Group,
		SourceTag:   item.Tag,
	}

	if item.IsGroup {
		state.Title = "#" + item.Group
		state.Items = app.buildGroupItems(item.Group)
	} else {
		state.Title = fmt.Sprintf("#%s{%s}", item.Group, item.Tag)
		state.Items = app.buildTagItems(item.Group, item.Tag)
	}

	if len(state.Items) == 0 {
		app.Message = "no files with this tag"
		return
	}

	app.MindmapState = state
	app.MindmapMode = true
}

func (app *AppState) buildGroupItems(group string) []MindmapItem {
	var items []MindmapItem

	// Find all files with any tag in this group
	var paths []string
	for path, fi := range app.Index.Files {
		if _, ok := fi.Tags[group]; ok {
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

func (app *AppState) buildTagItems(group, tag string) []MindmapItem {
	var items []MindmapItem

	// Find all files with this specific tag
	var paths []string
	for path, fi := range app.Index.Files {
		if tags, ok := fi.Tags[group]; ok {
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

// HandleMindmapEvent processes input in mindmap view
func (app *AppState) HandleMindmapEvent(ev terminal.Event) {
	state := app.MindmapState
	if state == nil {
		app.MindmapMode = false
		return
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.MindmapMode = false
		return

	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
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
		}

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

func (app *AppState) jumpMindmapToStart() {
	if app.MindmapState == nil {
		return
	}
	app.MindmapState.Cursor = 0
	app.MindmapState.Scroll = 0
}

func (app *AppState) jumpMindmapToEnd() {
	state := app.MindmapState
	if state == nil || len(state.Items) == 0 {
		return
	}
	state.Cursor = len(state.Items) - 1
	app.moveMindmapCursor(0)
}

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

// RenderMindmap draws the mindmap view
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

	rightInfo := "[Space:sel] [Esc:back]"
	drawText(cells, w, w-len(rightInfo)-1, 0, rightInfo, colorHeaderFg, colorHeaderBg, terminal.AttrNone)

	// Get expanded files for dependency highlighting
	depExpanded := make(map[string]bool)
	if app.ExpandDeps && len(app.Selected) > 0 {
		depExpanded = app.computeDepExpandedFiles()
	}

	// Content area
	contentTop := 1
	contentHeight := h - 2 // Header + help

	for i := 0; i < contentHeight && state.Scroll+i < len(state.Items); i++ {
		y := contentTop + i
		idx := state.Scroll + i
		item := state.Items[idx]

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
			drawText(cells, w, x, y, name, nameFg, rowBg, terminal.AttrNone)
			x += len(name) + 2

			// Tags (colored)
			if item.TagStr != "" && x < w-2 {
				drawColoredTags(cells, w, x, y, item.TagStr, rowBg)
			}
		}
	}

	// Help bar
	helpY := h - 1
	help := "j/k:nav  Space:toggle  a:all  c:clear  0/$:jump  Esc/q:back"
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

// drawColoredTags draws tags with distinct colors for groups and tag names
// Returns the x position after drawing
func drawColoredTags(cells []terminal.Cell, w, x, y int, tagStr string, bg terminal.RGB) int {
	if tagStr == "" || x >= w-1 {
		return x
	}

	maxX := w - 1
	i := 0
	runes := []rune(tagStr)
	n := len(runes)

	for i < n && x < maxX {
		if runes[i] == '#' {
			// Start of group: #groupname
			cells[y*w+x] = terminal.Cell{Rune: '#', Fg: colorGroupNameFg, Bg: bg}
			x++
			i++

			// Group name until '{' or space or end
			for i < n && x < maxX && runes[i] != '{' && runes[i] != ' ' {
				cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: colorGroupNameFg, Bg: bg}
				x++
				i++
			}

			// Opening brace
			if i < n && runes[i] == '{' && x < maxX {
				cells[y*w+x] = terminal.Cell{Rune: '{', Fg: colorGroupNameFg, Bg: bg}
				x++
				i++

				// Tags inside braces (cyan)
				for i < n && x < maxX && runes[i] != '}' {
					fg := colorTagNameFg
					if runes[i] == ',' {
						fg = colorGroupNameFg
					}
					cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: fg, Bg: bg}
					x++
					i++
				}

				// Closing brace
				if i < n && runes[i] == '}' && x < maxX {
					cells[y*w+x] = terminal.Cell{Rune: '}', Fg: colorGroupNameFg, Bg: bg}
					x++
					i++
				}
			}
		} else if runes[i] == ' ' {
			// Space between tag groups
			if x < maxX {
				cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
				x++
			}
			i++
		} else {
			// Unexpected character, just draw it
			cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: colorDefaultFg, Bg: bg}
			x++
			i++
		}
	}

	return x
}