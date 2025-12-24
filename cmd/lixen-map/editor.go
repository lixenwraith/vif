package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// BatchEditState holds state for multi-file tag editing overlay
type BatchEditState struct {
	// Source files (from app.Selected)
	Files      []string         // sorted paths
	FileErrors map[string]error // parse failures (render in red)

	// Merged tag tree from all files
	// category → group → module → tag → file coverage
	MergedTags map[string]*BatchCategory

	// Pending changes (uncommitted)
	Additions map[string]*BatchCategory // per-file additions
	Removals  map[string]*BatchCategory // per-file removals

	// Flattened tree for navigation
	Flat   []BatchTagItem
	Cursor int
	Scroll int

	// Expansion state
	Expanded map[string]bool // "cat" or "cat.grp" or "cat.grp.mod"

	// Input mode for new items
	InputMode   bool
	InputBuffer string
	InputLevel  BatchItemType // what level being added
	InputParent string        // parent key for context
}

type BatchCategory struct {
	Groups map[string]*BatchGroup
}

type BatchGroup struct {
	Modules map[string]*BatchModule
	// DirectTags for 2-level format (module="")
}

type BatchModule struct {
	Tags map[string]*BatchTagState
}

type BatchTagState struct {
	// Current state (on disk)
	CurrentFiles map[string]bool // files that have this tag
	// Pending state (after commit)
	PendingAdd    map[string]bool // files to add tag to
	PendingRemove map[string]bool // files to remove tag from
}

type BatchTagItem struct {
	Type     BatchItemType
	Key      string // "cat" or "cat.grp" or "cat.grp.mod" or "cat.grp.mod.tag"
	Category string
	Group    string
	Module   string
	Tag      string
	Expanded bool

	// Coverage info
	TotalFiles   int // files in selection
	CurrentCount int // files currently having this item
	PendingCount int // files that will have this item after commit
}

type BatchItemType uint8

const (
	BatchItemCategory BatchItemType = iota
	BatchItemGroup
	BatchItemModule
	BatchItemTag
)

// EnterEditMode initiates inline tag editing for the file at cursor
func (app *AppState) EnterEditMode() {
	if app.FocusPane != PaneTree {
		app.Message = "edit only from file tree"
		return
	}

	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]
	if node.IsDir {
		app.Message = "select a file to edit tags"
		return
	}

	app.EditTarget = node.Path
	app.EditMode = true

	content, err := readLixenLine(node.Path)
	if err != nil {
		app.Message = fmt.Sprintf("read error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		return
	}

	// Canonicalize for consistent editing
	categories, err := parseLixenContent(content)
	if err != nil {
		app.InputBuffer = content // Fallback to raw on parse error
		app.EditCursor = len(content)
		return
	}

	app.InputBuffer = canonicalizeLixenContent(categories)
	app.EditCursor = len(app.InputBuffer)
}

// HandleEditEvent processes keyboard input during tag editing
func (app *AppState) HandleEditEvent(ev terminal.Event) {
	buf := []rune(app.InputBuffer)
	cursor := app.EditCursor

	// Clamp cursor
	if cursor > len(buf) {
		cursor = len(buf)
	}
	if cursor < 0 {
		cursor = 0
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		app.EditCursor = 0
		app.Message = "edit cancelled"

	case terminal.KeyEnter:
		app.commitTagEdit()

	case terminal.KeyBackspace:
		if cursor > 0 {
			buf = append(buf[:cursor-1], buf[cursor:]...)
			cursor--
		}

	case terminal.KeyDelete:
		if cursor < len(buf) {
			buf = append(buf[:cursor], buf[cursor+1:]...)
		}

	case terminal.KeyLeft:
		if cursor > 0 {
			cursor--
		}

	case terminal.KeyRight:
		if cursor < len(buf) {
			cursor++
		}

	case terminal.KeyHome, terminal.KeyCtrlA:
		cursor = 0

	case terminal.KeyEnd, terminal.KeyCtrlE:
		cursor = len(buf)

	case terminal.KeyCtrlK:
		// Kill to end of line
		buf = buf[:cursor]

	case terminal.KeyCtrlU:
		// Kill to start of line
		buf = buf[cursor:]
		cursor = 0

	case terminal.KeyCtrlW:
		// Kill previous word
		if cursor > 0 {
			// Skip trailing spaces
			end := cursor
			for end > 0 && buf[end-1] == ' ' {
				end--
			}
			// Find word start
			start := end
			for start > 0 && buf[start-1] != ' ' {
				start--
			}
			buf = append(buf[:start], buf[end:]...)
			cursor = start
		}

	case terminal.KeyRune:
		// Insert at cursor
		newBuf := make([]rune, len(buf)+1)
		copy(newBuf[:cursor], buf[:cursor])
		newBuf[cursor] = ev.Rune
		copy(newBuf[cursor+1:], buf[cursor:])
		buf = newBuf
		cursor++
	}

	app.InputBuffer = string(buf)
	app.EditCursor = cursor
}

// commitTagEdit writes modified tags to file and triggers reindex
func (app *AppState) commitTagEdit() {
	path := app.EditTarget
	content := strings.TrimSpace(app.InputBuffer)

	if err := writeLixenLine(path, content); err != nil {
		app.Message = fmt.Sprintf("write error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		app.EditCursor = 0
		return
	}

	app.EditMode = false
	app.EditTarget = ""
	app.InputBuffer = ""
	app.EditCursor = 0

	app.ReindexAll()
	app.Message = fmt.Sprintf("updated: %s", path)
}

// readLixenLine extracts and merges all @lixen: content from file header
func readLixenLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var contents []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(trimmed, "package ") {
			break
		}

		if strings.HasPrefix(trimmed, "// @lixen:") {
			content := strings.TrimPrefix(trimmed, "// @lixen:")
			content = strings.TrimSpace(content)
			if content != "" {
				contents = append(contents, content)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	if len(contents) == 0 {
		return "", nil
	}

	// Merge multiple lines by parsing and re-canonicalizing
	merged := make(map[string]map[string]map[string][]string)

	for _, c := range contents {
		categories, err := parseLixenContent(c)
		if err != nil {
			continue // Skip malformed lines
		}
		for cat, groups := range categories {
			if merged[cat] == nil {
				merged[cat] = make(map[string]map[string][]string)
			}
			for g, mods := range groups {
				if merged[cat][g] == nil {
					merged[cat][g] = make(map[string][]string)
				}
				for m, tags := range mods {
					merged[cat][g][m] = appendUnique(merged[cat][g][m], tags...)
				}
			}
		}
	}

	return canonicalizeLixenContent(merged), nil
}

// appendUnique appends values not already present
func appendUnique(slice []string, values ...string) []string {
	seen := make(map[string]bool, len(slice))
	for _, s := range slice {
		seen[s] = true
	}
	for _, v := range values {
		if !seen[v] {
			slice = append(slice, v)
			seen[v] = true
		}
	}
	return slice
}

// parseLixenContent parses lixen content into category map
// Returns: category → group → module → tags
func parseLixenContent(content string) (map[string]map[string]map[string][]string, error) {
	if content == "" {
		return make(map[string]map[string]map[string][]string), nil
	}

	fi := &FileInfo{
		Tags: make(map[string]map[string]map[string][]string),
	}

	content = stripWhitespace(content)
	if err := parseBlocks(content, fi); err != nil {
		return nil, err
	}

	return fi.Tags, nil
}

// canonicalizeLixenContent creates canonical lixen content from category map
// Output format: #category1{...},#category2{...}
func canonicalizeLixenContent(categories map[string]map[string]map[string][]string) string {
	if len(categories) == 0 {
		return ""
	}

	// Sort category names
	catNames := make([]string, 0, len(categories))
	for cat := range categories {
		catNames = append(catNames, cat)
	}
	sort.Strings(catNames)

	var parts []string
	for _, cat := range catNames {
		groups := categories[cat]
		if block := formatCategoryBlock(cat, groups); block != "" {
			parts = append(parts, block)
		}
	}

	return strings.Join(parts, ",")
}

// formatCategoryBlock formats a single category block
func formatCategoryBlock(category string, groups map[string]map[string][]string) string {
	if len(groups) == 0 {
		return ""
	}

	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	var groupParts []string
	for _, g := range groupNames {
		mods := groups[g]

		// Check if only DirectTagsModule exists (2-level format)
		if len(mods) == 1 {
			if tags, ok := mods[DirectTagsModule]; ok && len(tags) > 0 {
				sortedTags := make([]string, len(tags))
				copy(sortedTags, tags)
				sort.Strings(sortedTags)
				groupParts = append(groupParts, fmt.Sprintf("%s(%s)", g, strings.Join(sortedTags, ",")))
				continue
			}
		}

		// 3-level format: group[mod1(tag1),mod2]
		modNames := make([]string, 0, len(mods))
		for m := range mods {
			if m != DirectTagsModule {
				modNames = append(modNames, m)
			}
		}
		sort.Strings(modNames)

		// Include DirectTagsModule tags as direct if present alongside modules
		var modParts []string
		if tags, ok := mods[DirectTagsModule]; ok && len(tags) > 0 {
			sortedTags := make([]string, len(tags))
			copy(sortedTags, tags)
			sort.Strings(sortedTags)
			// Direct tags go first without module wrapper
			for _, t := range sortedTags {
				modParts = append(modParts, t)
			}
		}

		for _, m := range modNames {
			tags := mods[m]
			if len(tags) == 0 {
				modParts = append(modParts, m)
			} else {
				sortedTags := make([]string, len(tags))
				copy(sortedTags, tags)
				sort.Strings(sortedTags)
				modParts = append(modParts, fmt.Sprintf("%s(%s)", m, strings.Join(sortedTags, ",")))
			}
		}

		if len(modParts) > 0 {
			groupParts = append(groupParts, fmt.Sprintf("%s[%s]", g, strings.Join(modParts, ",")))
		}
	}

	if len(groupParts) == 0 {
		return ""
	}

	return fmt.Sprintf("#%s{%s}", category, strings.Join(groupParts, ","))
}

// writeLixenLine atomically writes lixen line to file, removing all existing @lixen: lines
func writeLixenLine(path, content string) error {
	categories, err := parseLixenContent(content)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	canonical := canonicalizeLixenContent(categories)

	fileContent, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(fileContent), "\n")

	// Find all @lixen: lines and package line
	var lixenIndices []int
	packageIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// @lixen:") {
			lixenIndices = append(lixenIndices, i)
		}
		if strings.HasPrefix(trimmed, "package ") {
			packageIdx = i
			break
		}
	}

	var newLines []string

	if len(lixenIndices) > 0 {
		// Remove all existing @lixen: lines, insert single canonical at first position
		newLines = make([]string, 0, len(lines)-len(lixenIndices)+1)
		removed := make(map[int]bool)
		for _, idx := range lixenIndices {
			removed[idx] = true
		}

		insertDone := false
		for i, line := range lines {
			if removed[i] {
				// Insert canonical line at position of first removed line
				if !insertDone && canonical != "" {
					newLines = append(newLines, "// @lixen: "+canonical)
					insertDone = true
				}
				continue
			}
			newLines = append(newLines, line)
		}
	} else if canonical != "" {
		// No existing line - insert before package, after build tags
		insertIdx := 0
		if packageIdx > 0 {
			for i := 0; i < packageIdx; i++ {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == "" || strings.HasPrefix(trimmed, "//go:build") || strings.HasPrefix(trimmed, "// +build") {
					insertIdx = i + 1
				} else {
					break
				}
			}
		}

		newLines = make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertIdx]...)
		newLines = append(newLines, "// @lixen: "+canonical)
		newLines = append(newLines, lines[insertIdx:]...)
	} else {
		return nil // No content and no existing lines
	}

	result := strings.Join(newLines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	// Atomic write
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".lixen-edit-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(result); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if info, err := os.Stat(path); err == nil {
		os.Chmod(tmpPath, info.Mode())
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

func (app *AppState) EnterBatchEditMode() {
	files := make([]string, 0, len(app.Selected))
	for path := range app.Selected {
		files = append(files, path)
	}
	sort.Strings(files)

	state := &BatchEditState{
		Files:      files,
		FileErrors: make(map[string]error),
		MergedTags: make(map[string]*BatchCategory),
		Additions:  make(map[string]*BatchCategory),
		Removals:   make(map[string]*BatchCategory),
		Expanded:   make(map[string]bool),
	}

	// Parse and merge tags from all files
	for _, path := range files {
		if err := state.loadFileIntoMerged(path); err != nil {
			state.FileErrors[path] = err
		}
	}

	state.rebuildFlat()
	app.BatchEdit = state
	app.BatchEditMode = true
}

func (app *AppState) HandleBatchEditEvent(ev terminal.Event) {
	state := app.BatchEdit

	if state.InputMode {
		app.handleBatchInputEvent(ev)
		return
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.BatchEditMode = false
		app.BatchEdit = nil
		app.Message = "batch edit cancelled"

	case terminal.KeyCtrlS:
		succeeded, failed, err := state.writeAllChanges()
		if len(failed) > 0 {
			if err != nil {
				// Partial failure
				app.Message = fmt.Sprintf("partial write: %d failed, %d succeeded", len(failed), len(succeeded))
			} else {
				// All failed
				app.Message = fmt.Sprintf("write failed for %d files", len(failed))
			}
			return
		}
		app.BatchEditMode = false
		app.BatchEdit = nil
		app.ReindexAll()
		app.Message = fmt.Sprintf("updated %d files", len(succeeded))

	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			state.moveCursor(1)
		case 'k':
			state.moveCursor(-1)
		case 'h':
			state.collapse()
		case 'l':
			state.expand()
		case ' ':
			state.toggleCurrentItem()
		case 'i':
			state.startNewItemInput()
		case 'H':
			state.collapseAll()
		case 'L':
			state.expandAll()
		case '0':
			state.Cursor = 0
			state.Scroll = 0
		case '$':
			if len(state.Flat) > 0 {
				state.Cursor = len(state.Flat) - 1
			}
		}

	case terminal.KeyUp:
		state.moveCursor(-1)
	case terminal.KeyDown:
		state.moveCursor(1)
	case terminal.KeyLeft:
		state.collapse()
	case terminal.KeyRight:
		state.expand()
	case terminal.KeySpace:
		state.toggleCurrentItem()
	case terminal.KeyPageUp:
		state.moveCursor(-10)
	case terminal.KeyPageDown:
		state.moveCursor(10)
	case terminal.KeyHome:
		state.Cursor = 0
		state.Scroll = 0
	case terminal.KeyEnd:
		if len(state.Flat) > 0 {
			state.Cursor = len(state.Flat) - 1
		}
	}
}

// loadFileIntoMerged parses file and merges tags into BatchEditState
func (state *BatchEditState) loadFileIntoMerged(path string) error {
	content, err := readLixenLine(path) // existing function
	if err != nil {
		return err
	}

	categories, err := parseLixenContent(content) // existing function
	if err != nil {
		return err
	}

	// Merge into state.MergedTags
	for cat, groups := range categories {
		if state.MergedTags[cat] == nil {
			state.MergedTags[cat] = &BatchCategory{
				Groups: make(map[string]*BatchGroup),
			}
		}
		for grp, modules := range groups {
			if state.MergedTags[cat].Groups[grp] == nil {
				state.MergedTags[cat].Groups[grp] = &BatchGroup{
					Modules: make(map[string]*BatchModule),
				}
			}
			for mod, tags := range modules {
				if state.MergedTags[cat].Groups[grp].Modules[mod] == nil {
					state.MergedTags[cat].Groups[grp].Modules[mod] = &BatchModule{
						Tags: make(map[string]*BatchTagState),
					}
				}
				for _, tag := range tags {
					ts := state.MergedTags[cat].Groups[grp].Modules[mod].Tags[tag]
					if ts == nil {
						ts = &BatchTagState{
							CurrentFiles:  make(map[string]bool),
							PendingAdd:    make(map[string]bool),
							PendingRemove: make(map[string]bool),
						}
						state.MergedTags[cat].Groups[grp].Modules[mod].Tags[tag] = ts
					}
					ts.CurrentFiles[path] = true
				}
			}
		}
	}
	return nil
}

// toggleCurrentItem cycles selection state for item at cursor
func (state *BatchEditState) toggleCurrentItem() {
	if len(state.Flat) == 0 {
		return
	}

	item := state.Flat[state.Cursor]

	switch item.Type {
	case BatchItemTag:
		state.toggleTag(item.Category, item.Group, item.Module, item.Tag)
	case BatchItemModule:
		state.toggleModule(item.Category, item.Group, item.Module)
	case BatchItemGroup:
		state.toggleGroup(item.Category, item.Group)
	case BatchItemCategory:
		state.toggleCategory(item.Category)
	}

	state.rebuildFlat()
}

func (state *BatchEditState) toggleTag(cat, grp, mod, tag string) {
	ts := state.MergedTags[cat].Groups[grp].Modules[mod].Tags[tag]
	validFiles := state.validFiles()

	// Compute effective state (current + pending)
	effectiveCount := 0
	for _, path := range validFiles {
		has := ts.CurrentFiles[path]
		if ts.PendingAdd[path] {
			has = true
		}
		if ts.PendingRemove[path] {
			has = false
		}
		if has {
			effectiveCount++
		}
	}

	// Toggle logic: if all have it → remove all, else → add to all missing
	if effectiveCount == len(validFiles) {
		// Remove from all
		for _, path := range validFiles {
			if ts.CurrentFiles[path] {
				ts.PendingRemove[path] = true
				delete(ts.PendingAdd, path)
			} else {
				delete(ts.PendingAdd, path)
			}
		}
	} else {
		// Set to all missing
		for _, path := range validFiles {
			has := ts.CurrentFiles[path] && !ts.PendingRemove[path]
			has = has || ts.PendingAdd[path]
			if !has {
				if ts.CurrentFiles[path] {
					delete(ts.PendingRemove, path)
				} else {
					ts.PendingAdd[path] = true
				}
			}
		}
	}
}

func (state *BatchEditState) validFiles() []string {
	result := make([]string, 0, len(state.Files))
	for _, path := range state.Files {
		if state.FileErrors[path] == nil {
			result = append(result, path)
		}
	}
	return result
}

func (state *BatchEditState) startNewItemInput() {
	if len(state.Flat) == 0 {
		// No items, start at category level
		state.InputMode = true
		state.InputLevel = BatchItemCategory
		state.InputParent = ""
		state.InputBuffer = ""
		return
	}

	item := state.Flat[state.Cursor]
	state.InputMode = true
	state.InputBuffer = ""

	// Insert at same level as cursor
	switch item.Type {
	case BatchItemCategory:
		state.InputLevel = BatchItemCategory
		state.InputParent = ""
	case BatchItemGroup:
		state.InputLevel = BatchItemGroup
		state.InputParent = item.Category
	case BatchItemModule:
		state.InputLevel = BatchItemModule
		state.InputParent = item.Category + "." + item.Group
	case BatchItemTag:
		state.InputLevel = BatchItemTag
		state.InputParent = item.Category + "." + item.Group + "." + item.Module
	}
}

func (state *BatchEditState) computeFileChanges(path string) map[string]map[string]map[string][]string {
	// Build final tag structure for this file
	result := make(map[string]map[string]map[string][]string)

	for cat, catData := range state.MergedTags {
		for grp, grpData := range catData.Groups {
			for mod, modData := range grpData.Modules {
				for tag, ts := range modData.Tags {
					// Determine if file should have this tag
					has := ts.CurrentFiles[path]
					if ts.PendingAdd[path] {
						has = true
					}
					if ts.PendingRemove[path] {
						has = false
					}

					if has {
						if result[cat] == nil {
							result[cat] = make(map[string]map[string][]string)
						}
						if result[cat][grp] == nil {
							result[cat][grp] = make(map[string][]string)
						}
						result[cat][grp][mod] = append(result[cat][grp][mod], tag)
					}
				}
			}
		}
	}

	// Sort all tag lists
	for cat := range result {
		for grp := range result[cat] {
			for mod := range result[cat][grp] {
				sort.Strings(result[cat][grp][mod])
			}
		}
	}

	return result
}

func (state *BatchEditState) writeAllChanges() (succeeded, failed []string, err error) {
	validFiles := state.validFiles()

	// Phase 1: Compute all changes, validate writable
	changes := make(map[string]string) // path → new content
	for _, path := range validFiles {
		tags := state.computeFileChanges(path)
		content := canonicalizeLixenContent(tags) // existing function
		changes[path] = content
	}

	// Phase 2: Atomic write attempt
	for path, content := range changes {
		if err := writeLixenLine(path, content); err != nil {
			failed = append(failed, path)
		} else {
			succeeded = append(succeeded, path)
		}
	}

	if len(failed) > 0 && len(succeeded) > 0 {
		// Partial failure - caller decides rollback vs continue
		return succeeded, failed, fmt.Errorf("partial write: %d/%d failed", len(failed), len(changes))
	}

	return succeeded, failed, nil
}

func (app *AppState) renderBatchEdit(cells []terminal.Cell, w, h int) {
	state := app.BatchEdit

	// Background
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: colorDefaultBg}
	}

	// Border
	drawDoubleFrame(cells, w, 0, 0, w, h)

	// Header
	title := fmt.Sprintf(" BATCH EDIT (%d files) ", len(state.Files)-len(state.FileErrors))
	drawText(cells, w, 2, 0, title, colorHeaderFg, colorDefaultBg, terminal.AttrBold)

	hint := "Ctrl+S:save  Esc:cancel  i:new  Space:toggle"
	drawText(cells, w, w-len(hint)-2, 0, hint, colorHelpFg, colorDefaultBg, terminal.AttrNone)

	// Layout: Tree (40%) | Info (20%) | Files (40%)
	treeWidth := (w - 4) * 40 / 100
	infoWidth := (w - 4) * 20 / 100
	fileWidth := w - 4 - treeWidth - infoWidth

	treeX := 2
	infoX := treeX + treeWidth + 1
	fileX := infoX + infoWidth + 1

	contentY := 2
	contentH := h - 4

	// Vertical separators
	for y := contentY; y < contentY+contentH; y++ {
		cells[y*w+infoX-1] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
		cells[y*w+fileX-1] = terminal.Cell{Rune: boxV, Fg: colorPaneBorder, Bg: colorDefaultBg}
	}

	// Render three sections
	app.renderBatchTree(cells, w, treeX, contentY, treeWidth-1, contentH, state)
	app.renderBatchInfo(cells, w, infoX, contentY, infoWidth-1, contentH, state)
	app.renderBatchFiles(cells, w, fileX, contentY, fileWidth, contentH, state)

	// Input overlay if active
	if state.InputMode {
		app.renderBatchInput(cells, w, h, state)
	}
}

func (app *AppState) renderBatchTree(cells []terminal.Cell, totalW, x, y, w, h int, state *BatchEditState) {
	for i := 0; i < h && state.Scroll+i < len(state.Flat); i++ {
		// Adjust scroll based on actual visible height
		state.adjustScroll(h)

		row := y + i
		idx := state.Scroll + i
		item := state.Flat[idx]

		isCursor := idx == state.Cursor
		rowBg := colorDefaultBg
		if isCursor {
			rowBg = colorCursorBg
		}

		// Clear row
		for cx := x; cx < x+w; cx++ {
			cells[row*totalW+cx] = terminal.Cell{Rune: ' ', Bg: rowBg}
		}

		col := x
		indent := int(item.Type) * 2
		col += indent

		// Expand indicator for non-tags
		if item.Type != BatchItemTag {
			expandChar := '▶'
			if item.Expanded {
				expandChar = '▼'
			}
			cells[row*totalW+col] = terminal.Cell{Rune: expandChar, Fg: colorDirFg, Bg: rowBg}
			col += 2
		} else {
			col += 2 // align with expanded items
		}

		// Current state checkbox
		currentBox, currentFg := state.formatCurrentState(item)
		drawText(cells, totalW, col, row, currentBox, currentFg, rowBg, terminal.AttrNone)
		col += 4

		// Pending state indicator (arrow + new state)
		pendingBox, pendingFg, hasChange := state.formatPendingState(item)
		if hasChange {
			drawText(cells, totalW, col, row, "→", colorStatusFg, rowBg, terminal.AttrNone)
			col += 2
			drawText(cells, totalW, col, row, pendingBox, pendingFg, rowBg, terminal.AttrNone)
			col += 4
		} else {
			col += 6 // maintain alignment
		}

		// Label
		label := state.formatItemLabel(item)
		labelFg := state.itemLabelColor(item, hasChange)
		drawText(cells, totalW, col, row, label, labelFg, rowBg, terminal.AttrNone)
	}
}

// adjustScroll updates scroll offset for given visible height
func (state *BatchEditState) adjustScroll(visibleRows int) {
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

// Navigation methods for BatchEditState

func (state *BatchEditState) moveCursor(delta int) {
	if len(state.Flat) == 0 {
		return
	}
	state.Cursor += delta
	if state.Cursor < 0 {
		state.Cursor = 0
	}
	if state.Cursor >= len(state.Flat) {
		state.Cursor = len(state.Flat) - 1
	}
}

func (state *BatchEditState) collapse() {
	if len(state.Flat) == 0 {
		return
	}
	item := state.Flat[state.Cursor]
	if item.Type == BatchItemTag {
		// Tags don't expand, go to parent
		for i := state.Cursor - 1; i >= 0; i-- {
			if state.Flat[i].Type < item.Type {
				state.Cursor = i
				state.moveCursor(0)
				return
			}
		}
		return
	}
	if item.Expanded {
		state.Expanded[item.Key] = false
		state.rebuildFlat()
	} else {
		// Already collapsed, go to parent
		for i := state.Cursor - 1; i >= 0; i-- {
			if state.Flat[i].Type < item.Type {
				state.Cursor = i
				state.moveCursor(0)
				return
			}
		}
	}
}

func (state *BatchEditState) expand() {
	if len(state.Flat) == 0 {
		return
	}
	item := state.Flat[state.Cursor]
	if item.Type == BatchItemTag {
		return // Tags don't expand
	}
	if !item.Expanded {
		state.Expanded[item.Key] = true
		state.rebuildFlat()
	}
}

func (state *BatchEditState) collapseAll() {
	for key := range state.Expanded {
		state.Expanded[key] = false
	}
	state.rebuildFlat()
	state.Cursor = 0
	state.Scroll = 0
}

func (state *BatchEditState) expandAll() {
	for cat := range state.MergedTags {
		state.Expanded[cat] = true
		for grp := range state.MergedTags[cat].Groups {
			state.Expanded[cat+"."+grp] = true
			for mod := range state.MergedTags[cat].Groups[grp].Modules {
				if mod != DirectTagsModule {
					state.Expanded[cat+"."+grp+"."+mod] = true
				}
			}
		}
	}
	state.rebuildFlat()
}

// Coverage computation

func (state *BatchEditState) computeCategoryCoverage(cat string) (currentCount, pendingCount int) {
	validFiles := state.validFiles()
	currentSet := make(map[string]bool)
	pendingSet := make(map[string]bool)

	catData := state.MergedTags[cat]
	if catData == nil {
		return 0, 0
	}

	for _, grpData := range catData.Groups {
		for _, modData := range grpData.Modules {
			for _, ts := range modData.Tags {
				for _, path := range validFiles {
					if ts.CurrentFiles[path] {
						currentSet[path] = true
					}
					has := ts.CurrentFiles[path]
					if ts.PendingAdd[path] {
						has = true
					}
					if ts.PendingRemove[path] {
						has = false
					}
					if has {
						pendingSet[path] = true
					}
				}
			}
		}
	}
	return len(currentSet), len(pendingSet)
}

func (state *BatchEditState) computeGroupCoverage(cat, grp string) (currentCount, pendingCount int) {
	validFiles := state.validFiles()
	currentSet := make(map[string]bool)
	pendingSet := make(map[string]bool)

	grpData := state.MergedTags[cat].Groups[grp]
	if grpData == nil {
		return 0, 0
	}

	for _, modData := range grpData.Modules {
		for _, ts := range modData.Tags {
			for _, path := range validFiles {
				if ts.CurrentFiles[path] {
					currentSet[path] = true
				}
				has := ts.CurrentFiles[path]
				if ts.PendingAdd[path] {
					has = true
				}
				if ts.PendingRemove[path] {
					has = false
				}
				if has {
					pendingSet[path] = true
				}
			}
		}
	}
	return len(currentSet), len(pendingSet)
}

func (state *BatchEditState) computeModuleCoverage(cat, grp, mod string) (currentCount, pendingCount int) {
	validFiles := state.validFiles()
	currentSet := make(map[string]bool)
	pendingSet := make(map[string]bool)

	modData := state.MergedTags[cat].Groups[grp].Modules[mod]
	if modData == nil {
		return 0, 0
	}

	for _, ts := range modData.Tags {
		for _, path := range validFiles {
			if ts.CurrentFiles[path] {
				currentSet[path] = true
			}
			has := ts.CurrentFiles[path]
			if ts.PendingAdd[path] {
				has = true
			}
			if ts.PendingRemove[path] {
				has = false
			}
			if has {
				pendingSet[path] = true
			}
		}
	}
	return len(currentSet), len(pendingSet)
}

func (state *BatchEditState) computeTagCoverage(cat, grp, mod, tag string) (currentCount, pendingCount int) {
	ts := state.MergedTags[cat].Groups[grp].Modules[mod].Tags[tag]
	if ts == nil {
		return 0, 0
	}

	validFiles := state.validFiles()
	for _, path := range validFiles {
		if ts.CurrentFiles[path] {
			currentCount++
		}
		has := ts.CurrentFiles[path]
		if ts.PendingAdd[path] {
			has = true
		}
		if ts.PendingRemove[path] {
			has = false
		}
		if has {
			pendingCount++
		}
	}
	return currentCount, pendingCount
}

// State formatting for rendering

func (state *BatchEditState) formatCurrentState(item BatchTagItem) (string, terminal.RGB) {
	total := len(state.validFiles())
	if total == 0 {
		return "[ ]", colorUnselected
	}

	var current int
	switch item.Type {
	case BatchItemCategory:
		current, _ = state.computeCategoryCoverage(item.Category)
	case BatchItemGroup:
		current, _ = state.computeGroupCoverage(item.Category, item.Group)
	case BatchItemModule:
		current, _ = state.computeModuleCoverage(item.Category, item.Group, item.Module)
	case BatchItemTag:
		current, _ = state.computeTagCoverage(item.Category, item.Group, item.Module, item.Tag)
	}

	if current == 0 {
		return "[ ]", colorUnselected
	}
	if current == total {
		return "[x]", colorSelected
	}
	return "[o]", colorPartialSelectFg
}

func (state *BatchEditState) formatPendingState(item BatchTagItem) (box string, fg terminal.RGB, hasChange bool) {
	total := len(state.validFiles())
	if total == 0 {
		return "", colorUnselected, false
	}

	var current, pending int
	switch item.Type {
	case BatchItemCategory:
		current, pending = state.computeCategoryCoverage(item.Category)
	case BatchItemGroup:
		current, pending = state.computeGroupCoverage(item.Category, item.Group)
	case BatchItemModule:
		current, pending = state.computeModuleCoverage(item.Category, item.Group, item.Module)
	case BatchItemTag:
		current, pending = state.computeTagCoverage(item.Category, item.Group, item.Module, item.Tag)
	}

	if current == pending {
		return "", colorUnselected, false
	}

	if pending == 0 {
		return "[ ]", colorUnselected, true
	}
	if pending == total {
		return "[x]", colorSelected, true
	}
	return "[o]", colorPartialSelectFg, true
}

func (state *BatchEditState) formatItemLabel(item BatchTagItem) string {
	switch item.Type {
	case BatchItemCategory:
		return "#" + item.Category
	case BatchItemGroup:
		return item.Group
	case BatchItemModule:
		return item.Module
	case BatchItemTag:
		return item.Tag
	}
	return ""
}

func (state *BatchEditState) itemLabelColor(item BatchTagItem, hasChange bool) terminal.RGB {
	if hasChange {
		return colorExpandedFg // purple-ish for pending changes
	}
	switch item.Type {
	case BatchItemCategory:
		return colorGroupFg
	case BatchItemGroup:
		return colorGroupFg
	case BatchItemModule:
		return colorModuleFg
	case BatchItemTag:
		return colorTagFg
	}
	return colorDefaultFg
}

// Toggle methods for group/module/category

func (state *BatchEditState) toggleModule(cat, grp, mod string) {
	modData := state.MergedTags[cat].Groups[grp].Modules[mod]
	if modData == nil {
		return
	}
	validFiles := state.validFiles()

	// Count effective coverage
	effectiveCount := 0
	for _, path := range validFiles {
		hasAny := false
		for _, ts := range modData.Tags {
			has := ts.CurrentFiles[path]
			if ts.PendingAdd[path] {
				has = true
			}
			if ts.PendingRemove[path] {
				has = false
			}
			if has {
				hasAny = true
				break
			}
		}
		if hasAny {
			effectiveCount++
		}
	}

	// Toggle all tags in module
	if effectiveCount == len(validFiles) {
		// Remove from all
		for _, ts := range modData.Tags {
			for _, path := range validFiles {
				if ts.CurrentFiles[path] {
					ts.PendingRemove[path] = true
					delete(ts.PendingAdd, path)
				} else {
					delete(ts.PendingAdd, path)
				}
			}
		}
	} else {
		// Set to all
		for _, ts := range modData.Tags {
			for _, path := range validFiles {
				has := ts.CurrentFiles[path] && !ts.PendingRemove[path]
				has = has || ts.PendingAdd[path]
				if !has {
					if ts.CurrentFiles[path] {
						delete(ts.PendingRemove, path)
					} else {
						ts.PendingAdd[path] = true
					}
				}
			}
		}
	}
}

func (state *BatchEditState) toggleGroup(cat, grp string) {
	grpData := state.MergedTags[cat].Groups[grp]
	if grpData == nil {
		return
	}
	validFiles := state.validFiles()

	// Count effective coverage
	effectiveCount := 0
	for _, path := range validFiles {
		hasAny := false
		for _, modData := range grpData.Modules {
			for _, ts := range modData.Tags {
				has := ts.CurrentFiles[path]
				if ts.PendingAdd[path] {
					has = true
				}
				if ts.PendingRemove[path] {
					has = false
				}
				if has {
					hasAny = true
					break
				}
			}
			if hasAny {
				break
			}
		}
		if hasAny {
			effectiveCount++
		}
	}

	// Toggle all tags in all modules
	if effectiveCount == len(validFiles) {
		for _, modData := range grpData.Modules {
			for _, ts := range modData.Tags {
				for _, path := range validFiles {
					if ts.CurrentFiles[path] {
						ts.PendingRemove[path] = true
						delete(ts.PendingAdd, path)
					} else {
						delete(ts.PendingAdd, path)
					}
				}
			}
		}
	} else {
		for _, modData := range grpData.Modules {
			for _, ts := range modData.Tags {
				for _, path := range validFiles {
					has := ts.CurrentFiles[path] && !ts.PendingRemove[path]
					has = has || ts.PendingAdd[path]
					if !has {
						if ts.CurrentFiles[path] {
							delete(ts.PendingRemove, path)
						} else {
							ts.PendingAdd[path] = true
						}
					}
				}
			}
		}
	}
}

func (state *BatchEditState) toggleCategory(cat string) {
	catData := state.MergedTags[cat]
	if catData == nil {
		return
	}
	validFiles := state.validFiles()

	// Count effective coverage
	effectiveCount := 0
	for _, path := range validFiles {
		hasAny := false
		for _, grpData := range catData.Groups {
			for _, modData := range grpData.Modules {
				for _, ts := range modData.Tags {
					has := ts.CurrentFiles[path]
					if ts.PendingAdd[path] {
						has = true
					}
					if ts.PendingRemove[path] {
						has = false
					}
					if has {
						hasAny = true
						break
					}
				}
				if hasAny {
					break
				}
			}
			if hasAny {
				break
			}
		}
		if hasAny {
			effectiveCount++
		}
	}

	// Toggle all
	if effectiveCount == len(validFiles) {
		for _, grpData := range catData.Groups {
			for _, modData := range grpData.Modules {
				for _, ts := range modData.Tags {
					for _, path := range validFiles {
						if ts.CurrentFiles[path] {
							ts.PendingRemove[path] = true
							delete(ts.PendingAdd, path)
						} else {
							delete(ts.PendingAdd, path)
						}
					}
				}
			}
		}
	} else {
		for _, grpData := range catData.Groups {
			for _, modData := range grpData.Modules {
				for _, ts := range modData.Tags {
					for _, path := range validFiles {
						has := ts.CurrentFiles[path] && !ts.PendingRemove[path]
						has = has || ts.PendingAdd[path]
						if !has {
							if ts.CurrentFiles[path] {
								delete(ts.PendingRemove, path)
							} else {
								ts.PendingAdd[path] = true
							}
						}
					}
				}
			}
		}
	}
}

// Complete rebuildFlat implementation

func (state *BatchEditState) rebuildFlat() {
	state.Flat = nil
	totalFiles := len(state.Files) - len(state.FileErrors)

	cats := make([]string, 0, len(state.MergedTags))
	for cat := range state.MergedTags {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		catKey := cat
		catExpanded := state.Expanded[catKey]
		catCurrent, catPending := state.computeCategoryCoverage(cat)

		state.Flat = append(state.Flat, BatchTagItem{
			Type:         BatchItemCategory,
			Key:          catKey,
			Category:     cat,
			Expanded:     catExpanded,
			TotalFiles:   totalFiles,
			CurrentCount: catCurrent,
			PendingCount: catPending,
		})

		if !catExpanded {
			continue
		}

		grps := make([]string, 0, len(state.MergedTags[cat].Groups))
		for grp := range state.MergedTags[cat].Groups {
			grps = append(grps, grp)
		}
		sort.Strings(grps)

		for _, grp := range grps {
			grpKey := cat + "." + grp
			grpExpanded := state.Expanded[grpKey]
			grpCurrent, grpPending := state.computeGroupCoverage(cat, grp)

			state.Flat = append(state.Flat, BatchTagItem{
				Type:         BatchItemGroup,
				Key:          grpKey,
				Category:     cat,
				Group:        grp,
				Expanded:     grpExpanded,
				TotalFiles:   totalFiles,
				CurrentCount: grpCurrent,
				PendingCount: grpPending,
			})

			if !grpExpanded {
				continue
			}

			grpData := state.MergedTags[cat].Groups[grp]

			// Direct tags first (module == DirectTagsModule)
			if modData, ok := grpData.Modules[DirectTagsModule]; ok {
				tags := make([]string, 0, len(modData.Tags))
				for tag := range modData.Tags {
					tags = append(tags, tag)
				}
				sort.Strings(tags)

				for _, tag := range tags {
					tagCurrent, tagPending := state.computeTagCoverage(cat, grp, DirectTagsModule, tag)
					state.Flat = append(state.Flat, BatchTagItem{
						Type:         BatchItemTag,
						Key:          cat + "." + grp + ".." + tag,
						Category:     cat,
						Group:        grp,
						Module:       DirectTagsModule,
						Tag:          tag,
						TotalFiles:   totalFiles,
						CurrentCount: tagCurrent,
						PendingCount: tagPending,
					})
				}
			}

			// Named modules
			mods := make([]string, 0, len(grpData.Modules))
			for mod := range grpData.Modules {
				if mod != DirectTagsModule {
					mods = append(mods, mod)
				}
			}
			sort.Strings(mods)

			for _, mod := range mods {
				modKey := cat + "." + grp + "." + mod
				modExpanded := state.Expanded[modKey]
				modCurrent, modPending := state.computeModuleCoverage(cat, grp, mod)

				state.Flat = append(state.Flat, BatchTagItem{
					Type:         BatchItemModule,
					Key:          modKey,
					Category:     cat,
					Group:        grp,
					Module:       mod,
					Expanded:     modExpanded,
					TotalFiles:   totalFiles,
					CurrentCount: modCurrent,
					PendingCount: modPending,
				})

				if !modExpanded {
					continue
				}

				modData := grpData.Modules[mod]
				tags := make([]string, 0, len(modData.Tags))
				for tag := range modData.Tags {
					tags = append(tags, tag)
				}
				sort.Strings(tags)

				for _, tag := range tags {
					tagCurrent, tagPending := state.computeTagCoverage(cat, grp, mod, tag)
					state.Flat = append(state.Flat, BatchTagItem{
						Type:         BatchItemTag,
						Key:          cat + "." + grp + "." + mod + "." + tag,
						Category:     cat,
						Group:        grp,
						Module:       mod,
						Tag:          tag,
						TotalFiles:   totalFiles,
						CurrentCount: tagCurrent,
						PendingCount: tagPending,
					})
				}
			}
		}
	}

	if state.Cursor >= len(state.Flat) {
		state.Cursor = len(state.Flat) - 1
	}
	if state.Cursor < 0 {
		state.Cursor = 0
	}
}

// Input handling

func (app *AppState) handleBatchInputEvent(ev terminal.Event) {
	state := app.BatchEdit

	switch ev.Key {
	case terminal.KeyEscape:
		state.InputMode = false
		state.InputBuffer = ""

	case terminal.KeyEnter:
		if err := state.commitNewItem(); err != nil {
			app.Message = err.Error()
		}

	case terminal.KeyBackspace:
		if len(state.InputBuffer) > 0 {
			state.InputBuffer = state.InputBuffer[:len(state.InputBuffer)-1]
		}

	case terminal.KeyRune:
		state.InputBuffer += string(ev.Rune)
	}
}

func (state *BatchEditState) commitNewItem() error {
	name := strings.TrimSpace(state.InputBuffer)
	if name == "" {
		state.InputMode = false
		state.InputBuffer = ""
		return nil
	}

	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return fmt.Errorf("invalid character: %c", r)
		}
	}

	switch state.InputLevel {
	case BatchItemCategory:
		if state.MergedTags[name] != nil {
			return fmt.Errorf("category exists: %s", name)
		}
		state.MergedTags[name] = &BatchCategory{Groups: make(map[string]*BatchGroup)}
		state.Expanded[name] = true

	case BatchItemGroup:
		cat := state.InputParent
		if state.MergedTags[cat] == nil {
			return fmt.Errorf("category not found: %s", cat)
		}
		if state.MergedTags[cat].Groups[name] != nil {
			return fmt.Errorf("group exists: %s", name)
		}
		state.MergedTags[cat].Groups[name] = &BatchGroup{
			Modules: make(map[string]*BatchModule),
		}
		state.Expanded[cat+"."+name] = true

	case BatchItemModule:
		parts := strings.SplitN(state.InputParent, ".", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid parent: %s", state.InputParent)
		}
		cat, grp := parts[0], parts[1]
		if state.MergedTags[cat] == nil || state.MergedTags[cat].Groups[grp] == nil {
			return fmt.Errorf("group not found: %s.%s", cat, grp)
		}
		if state.MergedTags[cat].Groups[grp].Modules[name] != nil {
			return fmt.Errorf("module exists: %s", name)
		}
		state.MergedTags[cat].Groups[grp].Modules[name] = &BatchModule{
			Tags: make(map[string]*BatchTagState),
		}
		state.Expanded[cat+"."+grp+"."+name] = true

	case BatchItemTag:
		parts := strings.SplitN(state.InputParent, ".", 3)
		if len(parts) < 2 {
			return fmt.Errorf("invalid parent: %s", state.InputParent)
		}
		cat := parts[0]
		grp := parts[1]
		mod := DirectTagsModule
		if len(parts) == 3 {
			mod = parts[2]
		}
		if state.MergedTags[cat] == nil || state.MergedTags[cat].Groups[grp] == nil {
			return fmt.Errorf("group not found: %s.%s", cat, grp)
		}
		if state.MergedTags[cat].Groups[grp].Modules[mod] == nil {
			state.MergedTags[cat].Groups[grp].Modules[mod] = &BatchModule{
				Tags: make(map[string]*BatchTagState),
			}
		}
		if state.MergedTags[cat].Groups[grp].Modules[mod].Tags[name] != nil {
			return fmt.Errorf("tag exists: %s", name)
		}
		ts := &BatchTagState{
			CurrentFiles:  make(map[string]bool),
			PendingAdd:    make(map[string]bool),
			PendingRemove: make(map[string]bool),
		}
		// New tag is pending add for all valid files
		for _, path := range state.validFiles() {
			ts.PendingAdd[path] = true
		}
		state.MergedTags[cat].Groups[grp].Modules[mod].Tags[name] = ts
	}

	state.InputMode = false
	state.InputBuffer = ""
	state.rebuildFlat()
	return nil
}

// Rendering methods

func (app *AppState) renderBatchInfo(cells []terminal.Cell, totalW, x, y, w, h int, state *BatchEditState) {
	row := y

	// Section: Current item info
	if len(state.Flat) > 0 && state.Cursor < len(state.Flat) {
		item := state.Flat[state.Cursor]
		label := state.formatItemLabel(item)
		drawText(cells, totalW, x, row, "Selected:", colorStatusFg, colorDefaultBg, terminal.AttrNone)
		row++
		drawText(cells, totalW, x, row, label, colorHeaderFg, colorDefaultBg, terminal.AttrBold)
		row += 2
	}

	// Section: Pending changes summary
	drawText(cells, totalW, x, row, "Changes:", colorStatusFg, colorDefaultBg, terminal.AttrNone)
	row++

	addCount := 0
	removeCount := 0
	affectedFiles := make(map[string]bool)

	for _, catData := range state.MergedTags {
		for _, grpData := range catData.Groups {
			for _, modData := range grpData.Modules {
				for _, ts := range modData.Tags {
					for path := range ts.PendingAdd {
						addCount++
						affectedFiles[path] = true
					}
					for path := range ts.PendingRemove {
						removeCount++
						affectedFiles[path] = true
					}
				}
			}
		}
	}

	if addCount == 0 && removeCount == 0 {
		drawText(cells, totalW, x, row, "(none)", colorUnselected, colorDefaultBg, terminal.AttrDim)
	} else {
		if addCount > 0 {
			addStr := fmt.Sprintf("+%d additions", addCount)
			drawText(cells, totalW, x, row, addStr, colorSelected, colorDefaultBg, terminal.AttrNone)
			row++
		}
		if removeCount > 0 {
			remStr := fmt.Sprintf("-%d removals", removeCount)
			drawText(cells, totalW, x, row, remStr, colorAllTagFg, colorDefaultBg, terminal.AttrNone)
			row++
		}
		row++
		affStr := fmt.Sprintf("%d files", len(affectedFiles))
		drawText(cells, totalW, x, row, affStr, colorStatusFg, colorDefaultBg, terminal.AttrNone)
	}
}

func (app *AppState) renderBatchFiles(cells []terminal.Cell, totalW, x, y, w, h int, state *BatchEditState) {
	// Header
	drawText(cells, totalW, x, y, "Files:", colorStatusFg, colorDefaultBg, terminal.AttrBold)

	// Get files matching current cursor item
	var matchingFiles []string
	if len(state.Flat) > 0 && state.Cursor < len(state.Flat) {
		item := state.Flat[state.Cursor]
		matchingFiles = state.filesMatchingItem(item)
	}

	row := y + 1
	maxRows := h - 1
	colWidth := w / 2
	if colWidth < 20 {
		colWidth = w
	}

	col := 0
	displayed := 0
	for i, path := range state.Files {
		if displayed >= maxRows*2 {
			// Show overflow count
			remaining := len(state.Files) - displayed
			if remaining > 0 {
				overflowStr := fmt.Sprintf("(+%d more)", remaining)
				drawText(cells, totalW, x, y+maxRows, overflowStr, colorStatusFg, colorDefaultBg, terminal.AttrDim)
			}
			break
		}

		cellX := x + (col * colWidth)
		cellY := row

		// Determine color
		fg := colorDefaultFg
		if state.FileErrors[path] != nil {
			fg = terminal.RGB{R: 255, G: 80, B: 80} // red for errors
		} else {
			// Check if file matches current item
			isMatch := false
			for _, mp := range matchingFiles {
				if mp == path {
					isMatch = true
					break
				}
			}
			if isMatch {
				fg = colorSelected
			}
		}

		// Truncate path
		displayPath := filepath.Base(path)
		maxLen := colWidth - 1
		if len(displayPath) > maxLen && maxLen > 3 {
			displayPath = displayPath[:maxLen-1] + "…"
		}

		drawText(cells, totalW, cellX, cellY, displayPath, fg, colorDefaultBg, terminal.AttrNone)
		displayed++

		// Two-column layout
		col++
		if col >= 2 || colWidth == w {
			col = 0
			row++
		}

		_ = i
	}
}

func (state *BatchEditState) filesMatchingItem(item BatchTagItem) []string {
	var result []string
	validFiles := state.validFiles()

	switch item.Type {
	case BatchItemCategory:
		catData := state.MergedTags[item.Category]
		if catData == nil {
			return nil
		}
		for _, path := range validFiles {
			for _, grpData := range catData.Groups {
				found := false
				for _, modData := range grpData.Modules {
					for _, ts := range modData.Tags {
						has := ts.CurrentFiles[path]
						if ts.PendingAdd[path] {
							has = true
						}
						if ts.PendingRemove[path] {
							has = false
						}
						if has {
							result = append(result, path)
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if found {
					break
				}
			}
		}

	case BatchItemGroup:
		grpData := state.MergedTags[item.Category].Groups[item.Group]
		if grpData == nil {
			return nil
		}
		for _, path := range validFiles {
			found := false
			for _, modData := range grpData.Modules {
				for _, ts := range modData.Tags {
					has := ts.CurrentFiles[path]
					if ts.PendingAdd[path] {
						has = true
					}
					if ts.PendingRemove[path] {
						has = false
					}
					if has {
						result = append(result, path)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}

	case BatchItemModule:
		modData := state.MergedTags[item.Category].Groups[item.Group].Modules[item.Module]
		if modData == nil {
			return nil
		}
		for _, path := range validFiles {
			for _, ts := range modData.Tags {
				has := ts.CurrentFiles[path]
				if ts.PendingAdd[path] {
					has = true
				}
				if ts.PendingRemove[path] {
					has = false
				}
				if has {
					result = append(result, path)
					break
				}
			}
		}

	case BatchItemTag:
		ts := state.MergedTags[item.Category].Groups[item.Group].Modules[item.Module].Tags[item.Tag]
		if ts == nil {
			return nil
		}
		for _, path := range validFiles {
			has := ts.CurrentFiles[path]
			if ts.PendingAdd[path] {
				has = true
			}
			if ts.PendingRemove[path] {
				has = false
			}
			if has {
				result = append(result, path)
			}
		}
	}

	return result
}

func (app *AppState) renderBatchInput(cells []terminal.Cell, w, h int, state *BatchEditState) {
	// Center input box
	boxW := 40
	boxH := 3
	boxX := (w - boxW) / 2
	boxY := (h - boxH) / 2

	// Background
	drawRect(cells, boxX, boxY, boxW, boxH, w, colorInputBg)
	drawSingleBox(cells, w, boxX, boxY, boxW, boxH)

	// Label
	var label string
	switch state.InputLevel {
	case BatchItemCategory:
		label = "New category:"
	case BatchItemGroup:
		label = "New group:"
	case BatchItemModule:
		label = "New module:"
	case BatchItemTag:
		label = "New tag:"
	}
	drawText(cells, w, boxX+2, boxY+1, label, colorStatusFg, colorInputBg, terminal.AttrNone)

	// Input
	inputX := boxX + 2 + len(label) + 1
	inputW := boxW - len(label) - 5
	input := state.InputBuffer
	if len(input) > inputW {
		input = input[len(input)-inputW:]
	}
	drawText(cells, w, inputX, boxY+1, input, colorHeaderFg, colorInputBg, terminal.AttrNone)

	// Cursor
	cursorX := inputX + len(input)
	if cursorX < boxX+boxW-2 {
		cells[(boxY+1)*w+cursorX] = terminal.Cell{Rune: '_', Fg: colorHeaderFg, Bg: colorInputBg}
	}
}