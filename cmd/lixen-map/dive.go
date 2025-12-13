package main

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// DiveState holds computed relationship data for dive view
type DiveState struct {
	SourcePath string
	FileInfo   *FileInfo

	Panes      [4][]DiveListItem
	ActivePane DivePane
	Cursors    [4]int
	Scrolls    [4]int
	Expanded   map[string]bool // GroupKey → expanded (default true)
}

// DiveListItem represents a single row in any dive pane
type DiveListItem struct {
	Type     DiveItemType
	Label    string // Display text
	Path     string // Full file path for files, dir for dirs
	Indent   int
	Count    int    // Child count for expandable items
	GroupKey string // Collapse state key (empty for files)
	Expanded bool   // Current expansion state
}

// EnterDive transitions to dive view for the file at current mindmap cursor
func (app *AppState) EnterDive() {
	if app.MindmapState == nil || len(app.MindmapState.Items) == 0 {
		return
	}

	item := app.MindmapState.Items[app.MindmapState.Cursor]
	if item.IsDir || item.Path == "" {
		app.Message = "select a file to dive"
		return
	}

	state := computeDiveData(app, item.Path)
	if state == nil {
		app.Message = "no data for file"
		return
	}

	app.DiveState = state
	app.DiveMode = true
}

// ExitDive returns from dive view to mindmap view
func (app *AppState) ExitDive() {
	app.DiveMode = false
	app.DiveState = nil
}

// HandleDiveEvent processes keyboard input while in dive view
func (app *AppState) HandleDiveEvent(ev terminal.Event) {
	state := app.DiveState
	if state == nil {
		app.ExitDive()
		return
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.ExitDive()
	case terminal.KeyTab:
		state.ActivePane = (state.ActivePane + 1) % 4
	case terminal.KeyBacktab:
		state.ActivePane = (state.ActivePane + 3) % 4
	case terminal.KeyEnter:
		app.diveIntoSelected()
	case terminal.KeyRune:
		switch ev.Rune {
		case '?':
			app.HelpMode = true
		case 'q':
			app.ExitDive()
		case 'j':
			app.moveDiveCursor(1)
		case 'k':
			app.moveDiveCursor(-1)
		case 'h':
			app.collapseDiveItem()
		case 'l':
			app.expandDiveItem()
		case 'H':
			app.setAllDiveExpanded(false)
		case 'L':
			app.setAllDiveExpanded(true)
		case '0':
			app.jumpDiveCursor(0)
		case '$':
			app.jumpDiveCursor(-1)
		}
	case terminal.KeyUp:
		app.moveDiveCursor(-1)
	case terminal.KeyDown:
		app.moveDiveCursor(1)
	case terminal.KeyLeft:
		app.collapseDiveItem()
	case terminal.KeyRight:
		app.expandDiveItem()
	case terminal.KeyHome:
		app.jumpDiveCursor(0)
	case terminal.KeyEnd:
		app.jumpDiveCursor(-1)
	case terminal.KeyPageUp:
		app.moveDiveCursor(-10)
	case terminal.KeyPageDown:
		app.moveDiveCursor(10)
	}
}

// Navigation helpers

func (app *AppState) moveDiveCursor(delta int) {
	state := app.DiveState
	if state == nil {
		return
	}

	pane := state.ActivePane
	items := state.Panes[pane]
	if len(items) == 0 {
		return
	}

	cursor := state.Cursors[pane] + delta
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(items) {
		cursor = len(items) - 1
	}
	state.Cursors[pane] = cursor

	visibleH := app.diveContentHeight()
	scroll := state.Scrolls[pane]
	if cursor < scroll {
		scroll = cursor
	}
	if cursor >= scroll+visibleH {
		scroll = cursor - visibleH + 1
	}
	state.Scrolls[pane] = scroll
}

func (app *AppState) jumpDiveCursor(pos int) {
	state := app.DiveState
	if state == nil {
		return
	}

	pane := state.ActivePane
	items := state.Panes[pane]
	if len(items) == 0 {
		return
	}

	if pos < 0 {
		state.Cursors[pane] = len(items) - 1
	} else {
		state.Cursors[pane] = 0
		state.Scrolls[pane] = 0
	}
	app.moveDiveCursor(0) // Adjust scroll
}

func (app *AppState) diveContentHeight() int {
	h := app.Height - 5 // Header + info + separator + title row + help
	if h < 1 {
		h = 1
	}
	return h
}

// Collapse/Expand

func (app *AppState) collapseDiveItem() {
	state := app.DiveState
	if state == nil {
		return
	}

	pane := state.ActivePane
	items := state.Panes[pane]
	cursor := state.Cursors[pane]
	if cursor < 0 || cursor >= len(items) {
		return
	}

	item := items[cursor]

	// If on file, find parent expandable
	if item.GroupKey == "" {
		for i := cursor - 1; i >= 0; i-- {
			if items[i].GroupKey != "" {
				state.Cursors[pane] = i
				cursor = i
				item = items[i]
				break
			}
		}
	}

	if item.GroupKey != "" && state.Expanded[item.GroupKey] {
		state.Expanded[item.GroupKey] = false
		app.rebuildDivePane(pane)
	}
}

func (app *AppState) expandDiveItem() {
	state := app.DiveState
	if state == nil {
		return
	}

	pane := state.ActivePane
	items := state.Panes[pane]
	cursor := state.Cursors[pane]
	if cursor < 0 || cursor >= len(items) {
		return
	}

	item := items[cursor]
	if item.GroupKey != "" && !state.Expanded[item.GroupKey] {
		state.Expanded[item.GroupKey] = true
		app.rebuildDivePane(pane)
	}
}

func (app *AppState) setAllDiveExpanded(expanded bool) {
	state := app.DiveState
	if state == nil {
		return
	}

	for key := range state.Expanded {
		state.Expanded[key] = expanded
	}
	for i := 0; i < 4; i++ {
		app.rebuildDivePane(DivePane(i))
	}
}

// Dive into selected file

func (app *AppState) diveIntoSelected() {
	state := app.DiveState
	if state == nil {
		return
	}

	pane := state.ActivePane
	items := state.Panes[pane]
	cursor := state.Cursors[pane]
	if cursor < 0 || cursor >= len(items) {
		return
	}

	item := items[cursor]
	if item.Type != DiveItemFile || item.Path == "" {
		return
	}

	if app.Index.Files[item.Path] == nil {
		return
	}

	newState := computeDiveData(app, item.Path)
	if newState != nil {
		app.DiveState = newState
	}
}

// Data computation

func computeDiveData(app *AppState, path string) *DiveState {
	fi := app.Index.Files[path]
	if fi == nil {
		return nil
	}

	state := &DiveState{
		SourcePath: path,
		FileInfo:   fi,
		ActivePane: DivePaneDependsOn,
		Expanded:   make(map[string]bool),
	}

	fileDir := filepath.Dir(path)
	fileDir = filepath.ToSlash(fileDir)
	if fileDir == "." {
		fileDir = fi.Package
	}

	state.Panes[DivePaneDependsOn] = buildDepsItems(app, fi.Imports, true, state.Expanded)
	state.Panes[DivePaneDependedBy] = buildDepsItems(app, app.Index.ReverseDeps[fileDir], false, state.Expanded)
	state.Panes[DivePaneFocusLinks] = buildLinksItems(app, fi.Focus, path, "focus", state.Expanded)
	state.Panes[DivePaneInteractLinks] = buildLinksItems(app, fi.Interact, path, "interact", state.Expanded)

	return state
}

func buildDepsItems(app *AppState, source interface{}, forward bool, expanded map[string]bool) []DiveListItem {
	var items []DiveListItem
	var dirs []string

	if forward {
		imports := source.([]string)
		dirSet := make(map[string]bool)
		for _, impName := range imports {
			for dir, pkg := range app.Index.Packages {
				if pkg.Name == impName && !dirSet[dir] {
					dirSet[dir] = true
					dirs = append(dirs, dir)
					break
				}
			}
		}
	} else {
		dirs = source.([]string)
	}

	sort.Strings(dirs)

	for _, dir := range dirs {
		pkg := app.Index.Packages[dir]
		if pkg == nil {
			continue
		}

		key := "dep:" + dir
		if _, exists := expanded[key]; !exists {
			expanded[key] = true
		}

		files := make([]string, 0, len(pkg.Files))
		for _, f := range pkg.Files {
			files = append(files, filepath.Base(f.Path))
		}
		sort.Strings(files)

		items = append(items, DiveListItem{
			Type:     DiveItemDir,
			Label:    dir + "/",
			Path:     dir,
			Count:    len(files),
			GroupKey: key,
			Expanded: expanded[key],
		})

		if expanded[key] {
			for _, f := range files {
				items = append(items, DiveListItem{
					Type:   DiveItemFile,
					Label:  f,
					Path:   dir + "/" + f,
					Indent: 1,
				})
			}
		}
	}

	return items
}

func buildLinksItems(app *AppState, tagMap map[string][]string, selfPath, prefix string, expanded map[string]bool) []DiveListItem {
	var items []DiveListItem

	groups := make([]string, 0, len(tagMap))
	for g := range tagMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	for _, group := range groups {
		tags := tagMap[group]
		sortedTags := make([]string, len(tags))
		copy(sortedTags, tags)
		sort.Strings(sortedTags)

		groupFileCount := 0
		for _, tag := range sortedTags {
			groupFileCount += countLinkedFiles(app, group, tag, selfPath, prefix)
		}

		groupKey := prefix + ":" + group
		if _, exists := expanded[groupKey]; !exists {
			expanded[groupKey] = true
		}

		items = append(items, DiveListItem{
			Type:     DiveItemGroup,
			Label:    group,
			Count:    groupFileCount,
			GroupKey: groupKey,
			Expanded: expanded[groupKey],
		})

		if !expanded[groupKey] {
			continue
		}

		for _, tag := range sortedTags {
			files := findLinkedFiles(app, group, tag, selfPath, prefix)
			if len(files) == 0 {
				continue
			}

			tagKey := prefix + ":" + group + ":" + tag
			if _, exists := expanded[tagKey]; !exists {
				expanded[tagKey] = true
			}

			items = append(items, DiveListItem{
				Type:     DiveItemTag,
				Label:    tag,
				Indent:   1,
				Count:    len(files),
				GroupKey: tagKey,
				Expanded: expanded[tagKey],
			})

			if expanded[tagKey] {
				for _, f := range files {
					items = append(items, DiveListItem{
						Type:   DiveItemFile,
						Label:  f,
						Path:   f,
						Indent: 2,
					})
				}
			}
		}
	}

	return items
}

func countLinkedFiles(app *AppState, group, tag, selfPath, prefix string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if path == selfPath {
			continue
		}
		var tm map[string][]string
		if prefix == "focus" {
			tm = fi.Focus
		} else {
			tm = fi.Interact
		}
		if tags, ok := tm[group]; ok {
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

func findLinkedFiles(app *AppState, group, tag, selfPath, prefix string) []string {
	var files []string
	for path, fi := range app.Index.Files {
		if path == selfPath {
			continue
		}
		var tm map[string][]string
		if prefix == "focus" {
			tm = fi.Focus
		} else {
			tm = fi.Interact
		}
		if tags, ok := tm[group]; ok {
			for _, t := range tags {
				if t == tag {
					files = append(files, path)
					break
				}
			}
		}
	}
	sort.Strings(files)
	return files
}

func (app *AppState) rebuildDivePane(pane DivePane) {
	state := app.DiveState
	if state == nil {
		return
	}

	fileDir := filepath.Dir(state.SourcePath)
	fileDir = filepath.ToSlash(fileDir)
	if fileDir == "." {
		fileDir = state.FileInfo.Package
	}

	switch pane {
	case DivePaneDependsOn:
		state.Panes[pane] = buildDepsItems(app, state.FileInfo.Imports, true, state.Expanded)
	case DivePaneDependedBy:
		state.Panes[pane] = buildDepsItems(app, app.Index.ReverseDeps[fileDir], false, state.Expanded)
	case DivePaneFocusLinks:
		state.Panes[pane] = buildLinksItems(app, state.FileInfo.Focus, state.SourcePath, "focus", state.Expanded)
	case DivePaneInteractLinks:
		state.Panes[pane] = buildLinksItems(app, state.FileInfo.Interact, state.SourcePath, "interact", state.Expanded)
	}

	items := state.Panes[pane]
	if state.Cursors[pane] >= len(items) {
		state.Cursors[pane] = len(items) - 1
	}
	if state.Cursors[pane] < 0 {
		state.Cursors[pane] = 0
	}
}

// Rendering

func (app *AppState) RenderDive(cells []terminal.Cell, w, h int) {
	state := app.DiveState
	if state == nil {
		return
	}

	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	// Row 0: Header
	cells[0] = terminal.Cell{Rune: dboxTL, Fg: colorPaneBorder, Bg: colorDefaultBg}
	for x := 1; x < w-1; x++ {
		cells[x] = terminal.Cell{Rune: dboxH, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}
	cells[w-1] = terminal.Cell{Rune: dboxTR, Fg: colorPaneBorder, Bg: colorDefaultBg}

	title := fmt.Sprintf(" DIVE: %s ", state.SourcePath)
	if len(title) > w-4 {
		title = title[:w-7] + "... "
	}
	drawText(cells, w, 2, 0, title, colorHeaderFg, colorDefaultBg, terminal.AttrBold)

	// Row 1: Info bar
	cells[w] = terminal.Cell{Rune: dboxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
	cells[2*w-1] = terminal.Cell{Rune: dboxV, Fg: colorPaneBorder, Bg: colorDefaultBg}

	fi := state.FileInfo
	focusCount, interactCount := 0, 0
	for _, tags := range fi.Focus {
		focusCount += len(tags)
	}
	for _, tags := range fi.Interact {
		interactCount += len(tags)
	}
	info := fmt.Sprintf("pkg:%s │ Focus:%d │ Interact:%d │ Imports:%d",
		fi.Package, focusCount, interactCount, len(fi.Imports))
	if len(info) > w-4 {
		info = info[:w-7] + "..."
	}
	drawText(cells, w, 2, 1, info, colorStatusFg, colorDefaultBg, terminal.AttrNone)

	// Row 2: Separator
	cells[2*w] = terminal.Cell{Rune: dboxLT, Fg: colorPaneBorder, Bg: colorDefaultBg}
	for x := 1; x < w-1; x++ {
		cells[2*w+x] = terminal.Cell{Rune: dboxH, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}
	cells[3*w-1] = terminal.Cell{Rune: dboxRT, Fg: colorPaneBorder, Bg: colorDefaultBg}

	// Pane geometry: 4 equal-width panes
	paneW := (w - 5) / 4 // left border + 3 separators + right border
	paneX := [4]int{1, 1 + paneW + 1, 1 + 2*(paneW+1), 1 + 3*(paneW+1)}

	// Adjust last pane to fill remaining width
	lastPaneW := w - paneX[3] - 1

	// Separators at row 2
	for i := 1; i < 4; i++ {
		cells[2*w+paneX[i]-1] = terminal.Cell{Rune: boxTT, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	contentTop := 3
	contentH := h - 4 // Header + info + separator + help

	// Vertical borders
	for y := contentTop; y < contentTop+contentH; y++ {
		cells[y*w] = terminal.Cell{Rune: dboxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		cells[y*w+w-1] = terminal.Cell{Rune: dboxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		for i := 1; i < 4; i++ {
			cells[y*w+paneX[i]-1] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		}
	}

	// Pane titles and content
	titles := [4]string{"DEPENDS ON", "DEPENDED BY", "FOCUS LINKS", "INTERACT"}
	for i := 0; i < 4; i++ {
		active := state.ActivePane == DivePane(i)
		pw := paneW
		if i == 3 {
			pw = lastPaneW
		}
		renderDivePane(cells, w, paneX[i], contentTop, pw, contentH, titles[i],
			state.Panes[i], state.Cursors[i], state.Scrolls[i], active)
	}

	// Bottom frame
	helpY := h - 1
	cells[helpY*w] = terminal.Cell{Rune: dboxBL, Fg: colorPaneBorder, Bg: colorDefaultBg}
	for x := 1; x < w-1; x++ {
		cells[helpY*w+x] = terminal.Cell{Rune: dboxH, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}
	cells[helpY*w+w-1] = terminal.Cell{Rune: dboxBR, Fg: colorPaneBorder, Bg: colorDefaultBg}
	for i := 1; i < 4; i++ {
		cells[helpY*w+paneX[i]-1] = terminal.Cell{Rune: boxBT, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	help := " ?:help Tab:pane h/l:fold j/k:nav Enter:dive q:back ^Q:quit "
	drawText(cells, w, 2, helpY, help, colorHelpFg, colorDefaultBg, terminal.AttrNone)
}

func renderDivePane(cells []terminal.Cell, totalW, x, y, paneW, paneH int, title string, items []DiveListItem, cursor, scroll int, active bool) {
	bg := colorDefaultBg
	if active {
		bg = colorPaneActiveBg
	}

	// Clear pane
	for row := 0; row < paneH; row++ {
		for col := 0; col < paneW; col++ {
			idx := (y+row)*totalW + x + col
			if idx < len(cells) {
				cells[idx] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
			}
		}
	}

	// Title row
	titleFg := colorStatusFg
	if active {
		titleFg = colorHeaderFg
	}
	drawText(cells, totalW, x, y, truncateWithEllipsis(title, paneW-1), titleFg, bg, terminal.AttrBold)

	// Scroll indicator on title row
	if len(items) > paneH-1 && paneH > 1 {
		pct := 0
		if len(items) > 0 {
			pct = (scroll * 100) / len(items)
		}
		ind := fmt.Sprintf("%d%%", pct)
		indX := x + paneW - len(ind) - 1
		if indX > x+len(title)+1 {
			drawText(cells, totalW, indX, y, ind, colorStatusFg, bg, terminal.AttrDim)
		}
	}

	listY := y + 1
	listH := paneH - 1

	if len(items) == 0 {
		drawText(cells, totalW, x, listY, "(none)", colorUnselected, bg, terminal.AttrDim)
		return
	}

	for i := 0; i < listH && scroll+i < len(items); i++ {
		idx := scroll + i
		item := items[idx]
		row := listY + i

		isCursor := active && idx == cursor
		rowBg := bg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Clear row
		for col := 0; col < paneW; col++ {
			cellIdx := row*totalW + x + col
			if cellIdx < len(cells) {
				cells[cellIdx] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: rowBg}
			}
		}

		indent := item.Indent * 2
		col := x + indent

		// Expand indicator
		if item.GroupKey != "" {
			ch := '▶'
			if item.Expanded {
				ch = '▼'
			}
			if col < x+paneW {
				cells[row*totalW+col] = terminal.Cell{Rune: ch, Fg: colorDirFg, Bg: rowBg}
			}
			col += 2
		}

		// Label
		var fg terminal.RGB
		label := item.Label
		switch item.Type {
		case DiveItemDir:
			fg = colorDirFg
		case DiveItemGroup:
			fg = colorGroupFg
			if item.Count > 0 {
				label = fmt.Sprintf("%s (%d)", item.Label, item.Count)
			}
		case DiveItemTag:
			fg = colorTagFg
			if item.Count > 0 {
				label = fmt.Sprintf("%s (%d)", item.Label, item.Count)
			}
		case DiveItemFile:
			fg = colorDefaultFg
		}

		maxLen := paneW - (col - x) - 1
		if maxLen > 0 {
			drawText(cells, totalW, col, row, truncateWithEllipsis(label, maxLen), fg, rowBg, terminal.AttrNone)
		}
	}
}