package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

type EditorFocus int

const (
	EditorFocusTags  EditorFocus = iota // Start here - primary operation pane
	EditorFocusRaw                      // Tag input
	EditorFocusFiles                    // Read-only file list
)

// Add CoverageState type
type CoverageState uint8

const (
	CoverageNone CoverageState = iota
	CoveragePartial
	CoverageFull
)

type EditorState struct {
	Visible       bool
	SelectedFiles []string

	TagTree      []EditorTagNode
	TagNodes     []tui.TreeNode
	TagState     *tui.TreeState
	TagExpansion *tui.TreeExpansion

	Additions []TagRef
	Deletions []TagDeletion

	FocusPane EditorFocus
	FileState *tui.TreeState

	RawInput *tui.TextFieldState
}

type EditorTagNode struct {
	Type           TagItemType
	Category       string
	Group          string
	Module         string
	Tag            string
	Depth          int
	FileCount      int
	Total          int
	Coverage       CoverageState
	Deleted        bool
	ImplicitDelete bool
}

type TagRef struct {
	Category string
	Group    string
	Module   string
	Tag      string
}

type TagDeletion struct {
	TagRef
	Files []string
}

func NewEditorState(files []string) *EditorState {
	return &EditorState{
		Visible:       true,
		SelectedFiles: files,
		TagState:      tui.NewTreeState(20),
		TagExpansion:  tui.NewTreeExpansion(),
		FileState:     tui.NewTreeState(20),
		RawInput:      tui.NewTextFieldState(""),
		FocusPane:     EditorFocusTags, // Start at tags
	}
}

func (e *EditorState) Dirty() bool {
	return len(e.Additions) > 0 || len(e.Deletions) > 0
}

func (e *EditorState) CurrentTagNode() *EditorTagNode {
	if e.TagState.Cursor < 0 || e.TagState.Cursor >= len(e.TagTree) {
		return nil
	}
	return &e.TagTree[e.TagState.Cursor]
}

func (app *AppState) OpenEditor() {
	if len(app.Selected) == 0 {
		app.Message = "no files selected"
		return
	}

	files := make([]string, 0, len(app.Selected))
	for path := range app.Selected {
		files = append(files, path)
	}
	sort.Strings(files)

	app.Editor = NewEditorState(files)
	app.buildEditorTagTree()
}

func (app *AppState) CloseEditor() {
	if app.Editor != nil {
		app.Editor.Visible = false
		app.Editor = nil
	}
}

func (app *AppState) buildEditorTagTree() {
	if app.Editor == nil {
		return
	}

	e := app.Editor
	total := len(e.SelectedFiles)

	// CountEntity occurrences at each level
	type tagKey struct{ cat, group, mod, tag string }
	tagCounts := make(map[tagKey]int)
	modCounts := make(map[string]int)   // cat.group.mod -> count
	groupCounts := make(map[string]int) // cat.group -> count
	catCounts := make(map[string]int)   // cat -> count
	catSet := make(map[string]bool)

	for _, path := range e.SelectedFiles {
		fi := app.Index.Files[path]
		if fi == nil || fi.Tags == nil {
			continue
		}

		for cat, groups := range fi.Tags {
			catSet[cat] = true
			catCounts[cat]++

			for group, mods := range groups {
				if group != DirectTagsGroup {
					groupKey := cat + "." + group
					groupCounts[groupKey]++
				}

				for mod, tags := range mods {
					if mod != DirectTagsModule {
						modKey := cat + "." + group + "." + mod
						modCounts[modKey]++
					}

					for _, tag := range tags {
						tagCounts[tagKey{cat, group, mod, tag}]++
					}
				}
			}
		}
	}

	cats := make([]string, 0, len(catSet))
	for cat := range catSet {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	e.TagTree = nil

	for _, cat := range cats {
		catKey := "c:" + cat
		if _, known := e.TagExpansion.State[catKey]; !known {
			e.TagExpansion.Expand(catKey)
		}
		catExpanded := e.TagExpansion.IsExpanded(catKey)

		// Add category node
		e.TagTree = append(e.TagTree, EditorTagNode{
			Type:      TagItemTypeCategory,
			Category:  cat,
			Depth:     0,
			FileCount: catCounts[cat],
			Total:     total,
			Coverage:  computeCoverage(catCounts[cat], total),
		})

		if !catExpanded {
			continue
		}

		// Direct category tags (2-level)
		for tk, count := range tagCounts {
			if tk.cat == cat && tk.group == DirectTagsGroup && tk.mod == DirectTagsModule {
				e.TagTree = append(e.TagTree, EditorTagNode{
					Type:      TagItemTypeTag,
					Category:  cat,
					Group:     DirectTagsGroup,
					Module:    DirectTagsModule,
					Tag:       tk.tag,
					Depth:     1,
					FileCount: count,
					Total:     total,
					Coverage:  computeCoverage(count, total),
				})
			}
		}

		// Groups (excluding DirectTagsGroup)
		groupSet := make(map[string]bool)
		for key := range groupCounts {
			if strings.HasPrefix(key, cat+".") {
				group := strings.TrimPrefix(key, cat+".")
				if group != DirectTagsGroup {
					groupSet[group] = true
				}
			}
		}
		groups := make([]string, 0, len(groupSet))
		for g := range groupSet {
			groups = append(groups, g)
		}
		sort.Strings(groups)

		for _, group := range groups {
			groupKey := "g:" + cat + "." + group
			if _, known := e.TagExpansion.State[groupKey]; !known {
				e.TagExpansion.Expand(groupKey)
			}
			groupExpanded := e.TagExpansion.IsExpanded(groupKey)
			gCount := groupCounts[cat+"."+group]

			e.TagTree = append(e.TagTree, EditorTagNode{
				Type:      TagItemTypeGroup,
				Category:  cat,
				Group:     group,
				Depth:     1,
				FileCount: gCount,
				Total:     total,
				Coverage:  computeCoverage(gCount, total),
			})

			if !groupExpanded {
				continue
			}

			// Direct group tags (3-level)
			for tk, count := range tagCounts {
				if tk.cat == cat && tk.group == group && tk.mod == DirectTagsModule {
					e.TagTree = append(e.TagTree, EditorTagNode{
						Type:      TagItemTypeTag,
						Category:  cat,
						Group:     group,
						Module:    DirectTagsModule,
						Tag:       tk.tag,
						Depth:     2,
						FileCount: count,
						Total:     total,
						Coverage:  computeCoverage(count, total),
					})
				}
			}

			// Modules
			modSet := make(map[string]bool)
			for key := range modCounts {
				prefix := cat + "." + group + "."
				if strings.HasPrefix(key, prefix) {
					mod := strings.TrimPrefix(key, prefix)
					if mod != "" {
						modSet[mod] = true
					}
				}
			}
			mods := make([]string, 0, len(modSet))
			for m := range modSet {
				mods = append(mods, m)
			}
			sort.Strings(mods)

			for _, mod := range mods {
				modKey := "m:" + cat + "." + group + "." + mod
				if _, known := e.TagExpansion.State[modKey]; !known {
					e.TagExpansion.Expand(modKey)
				}
				modExpanded := e.TagExpansion.IsExpanded(modKey)
				mCount := modCounts[cat+"."+group+"."+mod]

				e.TagTree = append(e.TagTree, EditorTagNode{
					Type:      TagItemTypeModule,
					Category:  cat,
					Group:     group,
					Module:    mod,
					Depth:     2,
					FileCount: mCount,
					Total:     total,
					Coverage:  computeCoverage(mCount, total),
				})

				if !modExpanded {
					continue
				}

				// Module tags (4-level)
				var modTags []string
				for tk := range tagCounts {
					if tk.cat == cat && tk.group == group && tk.mod == mod {
						modTags = append(modTags, tk.tag)
					}
				}
				sort.Strings(modTags)

				for _, tag := range modTags {
					count := tagCounts[tagKey{cat, group, mod, tag}]
					e.TagTree = append(e.TagTree, EditorTagNode{
						Type:      TagItemTypeTag,
						Category:  cat,
						Group:     group,
						Module:    mod,
						Tag:       tag,
						Depth:     3,
						FileCount: count,
						Total:     total,
						Coverage:  computeCoverage(count, total),
					})
				}
			}
		}
	}

	// Restore deletion state
	for i := range e.TagTree {
		node := &e.TagTree[i]
		for _, del := range e.Deletions {
			if node.Category == del.Category && node.Group == del.Group &&
				node.Module == del.Module && node.Tag == del.Tag {
				node.Deleted = true
				break
			}
		}
	}

	e.TagState.AdjustScroll(len(e.TagTree))
}

func computeCoverage(count, total int) CoverageState {
	if count == 0 || total == 0 {
		return CoverageNone
	}
	if count == total {
		return CoverageFull
	}
	return CoveragePartial
}

func (app *AppState) buildEditorTreeNodes() []tui.TreeNode {
	e := app.Editor
	if e == nil {
		return nil
	}

	nodes := make([]tui.TreeNode, 0, len(e.TagTree))

	for i, item := range e.TagTree {
		var label string
		var expandable bool
		var key string

		switch item.Type {
		case TagItemTypeCategory:
			label = "#" + item.Category
			expandable = true
			key = "c:" + item.Category
		case TagItemTypeGroup:
			label = item.Group
			expandable = true
			key = "g:" + item.Category + "." + item.Group
		case TagItemTypeModule:
			label = item.Module
			expandable = true
			key = "m:" + item.Category + "." + item.Group + "." + item.Module
		case TagItemTypeTag:
			label = item.Tag
			expandable = false
			key = fmt.Sprintf("t:%s.%s.%s.%s", item.Category, item.Group, item.Module, item.Tag)
		}

		var suffix string
		if item.Type == TagItemTypeTag || item.Type == TagItemTypeGroup {
			switch item.Coverage {
			case CoverageFull:
				suffix = " [ALL]"
			case CoveragePartial:
				suffix = fmt.Sprintf(" [%d/%d]", item.FileCount, item.Total)
			}
		}

		fg := app.Theme.Fg
		attr := terminal.AttrNone
		if item.Deleted || item.ImplicitDelete {
			fg = app.Theme.Unselected
			attr = terminal.AttrDim
		} else if item.Type != TagItemTypeTag {
			fg = app.Theme.GroupFg
			attr = terminal.AttrBold
		}

		check := tui.CheckNone
		checkFg := app.Theme.Unselected
		if item.Deleted {
			check = tui.CheckFull
			checkFg = app.Theme.Error
		} else if item.ImplicitDelete {
			check = tui.CheckPartial
			checkFg = app.Theme.Warning
		}

		suffixText := suffix
		if item.ImplicitDelete {
			suffixText = " (will be empty)"
		}

		nodes = append(nodes, tui.TreeNode{
			Key:         key,
			Label:       label,
			Expandable:  expandable,
			Expanded:    e.TagExpansion.IsExpanded(key),
			Depth:       item.Depth,
			Check:       check,
			CheckFg:     checkFg,
			Style:       tui.Style{Fg: fg, Attr: attr},
			Suffix:      suffixText,
			SuffixStyle: tui.Style{Fg: app.Theme.StatusFg},
			Data:        i,
		})
	}

	return nodes
}

func (app *AppState) handleEditorEvent(ev terminal.Event) bool {
	e := app.Editor
	if e == nil || !e.Visible {
		return false
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.CloseEditor()
		return true

	case terminal.KeyCtrlS:
		if e.Dirty() {
			app.executeEditorSave() // Direct save, no confirmation
		} else {
			app.CloseEditor()
		}
		return true

	case terminal.KeyTab:
		// Cycle: Tags → Raw → Files → Tags
		e.FocusPane = (e.FocusPane + 1) % 3
		return true

	case terminal.KeyBacktab:
		e.FocusPane = (e.FocusPane + 2) % 3
		return true

	case terminal.KeyRune:
		if ev.Rune == '?' {
			app.ToggleHelp()
			return true
		}
	}

	switch e.FocusPane {
	case EditorFocusTags:
		app.handleEditorTagsEvent(ev)
	case EditorFocusRaw:
		app.handleEditorRawEvent(ev)
	case EditorFocusFiles:
		app.handleEditorFilesEvent(ev)
	}

	return true
}

func (app *AppState) handleEditorFilesEvent(ev terminal.Event) {
	e := app.Editor
	total := len(e.SelectedFiles)

	switch ev.Key {
	case terminal.KeyUp:
		e.FileState.MoveCursor(-1, total)
	case terminal.KeyDown:
		e.FileState.MoveCursor(1, total)
	case terminal.KeyPageUp:
		e.FileState.PageUp(total)
	case terminal.KeyPageDown:
		e.FileState.PageDown(total)
	case terminal.KeyHome:
		e.FileState.JumpStart()
	case terminal.KeyEnd:
		e.FileState.JumpEnd(total)
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			e.FileState.MoveCursor(1, total)
		case 'k':
			e.FileState.MoveCursor(-1, total)
		case 'g':
			e.FileState.JumpStart()
		case 'G':
			e.FileState.JumpEnd(total)
		}
	}
}

func (app *AppState) handleEditorTagsEvent(ev terminal.Event) {
	e := app.Editor
	total := len(e.TagTree)

	switch ev.Key {
	case terminal.KeyUp:
		e.TagState.MoveCursor(-1, total)
	case terminal.KeyDown:
		e.TagState.MoveCursor(1, total)
	case terminal.KeyPageUp:
		e.TagState.PageUp(total)
	case terminal.KeyPageDown:
		e.TagState.PageDown(total)
	case terminal.KeyHome:
		e.TagState.JumpStart()
	case terminal.KeyEnd:
		e.TagState.JumpEnd(total)
	case terminal.KeyLeft:
		app.collapseEditorNode()
	case terminal.KeyRight:
		app.expandEditorNode()
	case terminal.KeySpace:
		app.toggleEditorDeletion()
	case terminal.KeyRune:
		switch ev.Rune {
		case 'j':
			e.TagState.MoveCursor(1, total)
		case 'k':
			e.TagState.MoveCursor(-1, total)
		case 'h':
			app.collapseEditorNode()
		case 'l':
			app.expandEditorNode()
		case 'g':
			e.TagState.JumpStart()
		case 'G':
			e.TagState.JumpEnd(total)
		case ' ', 'd':
			app.toggleEditorDeletion()
		}
	}
}

func (app *AppState) handleEditorRawEvent(ev terminal.Event) {
	e := app.Editor

	switch ev.Key {
	case terminal.KeyEnter:
		app.addTagFromRaw()
		return
	}

	e.RawInput.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
}

func (app *AppState) collapseEditorNode() {
	e := app.Editor
	node := e.CurrentTagNode()
	if node == nil {
		return
	}

	var key string
	switch {
	case node.Group == "":
		key = "c:" + node.Category
	case node.Module == "" && node.Tag == "":
		key = "g:" + node.Category + "." + node.Group
	case node.Tag == "":
		key = "m:" + node.Category + "." + node.Group + "." + node.Module
	default:
		for i := e.TagState.Cursor - 1; i >= 0; i-- {
			n := &e.TagTree[i]
			if n.Depth < node.Depth {
				e.TagState.Cursor = i
				e.TagState.AdjustScroll(len(e.TagTree))
				return
			}
		}
		return
	}

	if e.TagExpansion.IsExpanded(key) {
		e.TagExpansion.Collapse(key)
		app.buildEditorTagTree()
	} else {
		for i := e.TagState.Cursor - 1; i >= 0; i-- {
			if e.TagTree[i].Depth < node.Depth {
				e.TagState.Cursor = i
				e.TagState.AdjustScroll(len(e.TagTree))
				return
			}
		}
	}
}

func (app *AppState) expandEditorNode() {
	e := app.Editor
	node := e.CurrentTagNode()
	if node == nil || node.Tag != "" {
		return
	}

	var key string
	switch {
	case node.Group == "":
		key = "c:" + node.Category
	case node.Module == "" && node.Tag == "":
		key = "g:" + node.Category + "." + node.Group
	case node.Tag == "":
		key = "m:" + node.Category + "." + node.Group + "." + node.Module
	}

	if !e.TagExpansion.IsExpanded(key) {
		e.TagExpansion.Expand(key)
		app.buildEditorTagTree()
	}
}

func (app *AppState) toggleEditorDeletion() {
	e := app.Editor
	node := e.CurrentTagNode()
	if node == nil {
		return
	}

	// Collect all affected tags based on hierarchy level
	var targets []TagRef
	for i := range e.TagTree {
		n := &e.TagTree[i]
		if !app.nodeMatchesHierarchy(node, n) {
			continue
		}
		targets = append(targets, TagRef{
			Category: n.Category,
			Group:    n.Group,
			Module:   n.Module,
			Tag:      n.Tag,
		})
	}

	if len(targets) == 0 {
		return
	}

	// Check if already marked - toggle state
	alreadyDeleted := node.Deleted

	for _, ref := range targets {
		if alreadyDeleted {
			// RemoveComponent from deletions
			app.removeFromDeletions(ref)
		} else {
			// Add to deletions
			app.addToDeletions(ref)
		}
	}

	// Update visual state for all matching nodes
	for i := range e.TagTree {
		n := &e.TagTree[i]
		if app.nodeMatchesHierarchy(node, n) {
			n.Deleted = !alreadyDeleted
		}
	}

	app.computeImplicitDeletions()
}

func (app *AppState) computeImplicitDeletions() {
	e := app.Editor
	if e == nil {
		return
	}

	// Reset implicit flags
	for i := range e.TagTree {
		e.TagTree[i].ImplicitDelete = false
	}

	// Build child counts and deleted counts per parent
	type nodeKey struct{ cat, group, module string }
	childCount := make(map[nodeKey]int)
	deletedCount := make(map[nodeKey]int)

	for i := range e.TagTree {
		n := &e.TagTree[i]
		switch n.Type {
		case TagItemTypeTag:
			key := nodeKey{n.Category, n.Group, n.Module}
			childCount[key]++
			if n.Deleted {
				deletedCount[key]++
			}
		case TagItemTypeModule:
			key := nodeKey{n.Category, n.Group, ""}
			childCount[key]++
		case TagItemTypeGroup:
			key := nodeKey{n.Category, "", ""}
			childCount[key]++
		}
	}

	// Mark modules implicit if all tags deleted
	for i := range e.TagTree {
		n := &e.TagTree[i]
		if n.Type == TagItemTypeModule && !n.Deleted {
			key := nodeKey{n.Category, n.Group, n.Module}
			if childCount[key] > 0 && deletedCount[key] == childCount[key] {
				n.ImplicitDelete = true
				// Propagate to parent group
				groupKey := nodeKey{n.Category, n.Group, ""}
				deletedCount[groupKey]++
			}
		}
	}

	// Mark groups implicit if all modules/direct-tags deleted
	for i := range e.TagTree {
		n := &e.TagTree[i]
		if n.Type == TagItemTypeGroup && !n.Deleted {
			key := nodeKey{n.Category, n.Group, ""}
			if childCount[key] > 0 && deletedCount[key] == childCount[key] {
				n.ImplicitDelete = true
				catKey := nodeKey{n.Category, "", ""}
				deletedCount[catKey]++
			}
		}
	}
}

// nodeMatchesHierarchy returns true if child is under parent in hierarchy
func (app *AppState) nodeMatchesHierarchy(parent, child *EditorTagNode) bool {
	// Category level - matches all in category
	if parent.Group == "" {
		return child.Category == parent.Category
	}
	// Group level - matches all in group
	if parent.Module == "" && parent.Tag == "" {
		return child.Category == parent.Category && child.Group == parent.Group
	}
	// Module level - matches all in module
	if parent.Tag == "" {
		return child.Category == parent.Category &&
			child.Group == parent.Group &&
			child.Module == parent.Module
	}
	// Tag level - exact match
	return child.Category == parent.Category &&
		child.Group == parent.Group &&
		child.Module == parent.Module &&
		child.Tag == parent.Tag
}

func (app *AppState) addToDeletions(ref TagRef) {
	e := app.Editor
	// Check if already pending
	for _, del := range e.Deletions {
		if del.Category == ref.Category && del.Group == ref.Group &&
			del.Module == ref.Module && del.Tag == ref.Tag {
			return
		}
	}

	// Find files that have this tag
	var files []string
	for _, path := range e.SelectedFiles {
		if app.fileHasRef(path, ref) {
			files = append(files, path)
		}
	}

	if len(files) > 0 {
		e.Deletions = append(e.Deletions, TagDeletion{TagRef: ref, Files: files})
	}
}

// fileHasRef checks if file has content at or under the ref's hierarchy level
func (app *AppState) fileHasRef(path string, ref TagRef) bool {
	fi := app.Index.Files[path]
	if fi == nil {
		return false
	}

	groups := fi.CategoryTags(ref.Category)
	if groups == nil {
		return false
	}
	if ref.Group == "" {
		return true // Category match
	}

	mods, ok := groups[ref.Group]
	if !ok {
		return false
	}
	if ref.Module == "" {
		return true // Group match
	}

	tags, ok := mods[ref.Module]
	if !ok {
		return false
	}
	if ref.Tag == "" {
		return true // Module match
	}

	for _, t := range tags {
		if t == ref.Tag {
			return true // Tag match
		}
	}
	return false
}

func (app *AppState) removeFromDeletions(ref TagRef) {
	e := app.Editor
	for i, del := range e.Deletions {
		if del.Category == ref.Category && del.Group == ref.Group &&
			del.Module == ref.Module && del.Tag == ref.Tag {
			e.Deletions = append(e.Deletions[:i], e.Deletions[i+1:]...)
			return
		}
	}
}

func (app *AppState) addTagFromRaw() {
	e := app.Editor
	input := strings.TrimSpace(e.RawInput.Value())
	if input == "" {
		return
	}

	fi := &FileInfo{Tags: make(map[string]map[string]map[string][]string)}
	content := stripWhitespace(input)
	if !strings.HasPrefix(content, "#") {
		content = "#" + content
	}

	if err := parseBlocks(content, fi); err != nil {
		app.Message = fmt.Sprintf("parse error: %v", err)
		return
	}

	count := 0
	for cat, groups := range fi.Tags {
		for group, mods := range groups {
			for mod, tags := range mods {
				if len(tags) == 0 {
					// Module without tags - skip or handle as needed
					continue
				}
				for _, tag := range tags {
					ref := TagRef{Category: cat, Group: group, Module: mod, Tag: tag}
					if !app.hasAddition(ref) {
						e.Additions = append(e.Additions, ref)
						count++
					}
				}
			}
		}
	}

	e.RawInput.Clear()
	if count > 0 {
		app.Message = fmt.Sprintf("added %d tag entries", count)
	}
}

func (app *AppState) hasAddition(ref TagRef) bool {
	for _, add := range app.Editor.Additions {
		if add == ref {
			return true
		}
	}
	return false
}

func formatTagRef(ref TagRef) string {
	if ref.Tag == "" && ref.Module == "" {
		return fmt.Sprintf("#%s{%s}", ref.Category, ref.Group)
	}
	if ref.Tag == "" {
		return fmt.Sprintf("#%s{%s[%s]}", ref.Category, ref.Group, ref.Module)
	}
	if ref.Module == "" {
		return fmt.Sprintf("#%s{%s(%s)}", ref.Category, ref.Group, ref.Tag)
	}
	return fmt.Sprintf("#%s{%s[%s(%s)]}", ref.Category, ref.Group, ref.Module, ref.Tag)
}

func (app *AppState) executeEditorSave() {
	e := app.Editor
	if e == nil {
		return
	}

	modified := 0
	for _, path := range e.SelectedFiles {
		if app.applyTagChangesToFile(path, e.Additions, e.Deletions) {
			modified++
		}
	}

	app.ReindexAll()
	app.Message = fmt.Sprintf("modified %d files", modified)
	app.CloseEditor()
}

func (app *AppState) applyTagChangesToFile(path string, additions []TagRef, deletions []TagDeletion) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	hasTrailingNewline := len(content) > 0 && content[len(content)-1] == '\n'

	lines := strings.Split(string(content), "\n")
	var hierarchyLineIdx = -1
	var packageLineIdx = -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		prefix := "@" + HierarchyLinePrefix + ":"
		if strings.HasPrefix(trimmed, "// "+prefix) {
			hierarchyLineIdx = i
		}
		if packageLineIdx == -1 && strings.HasPrefix(trimmed, "package ") {
			packageLineIdx = i
		}
	}

	// Deep copy existing tags from index
	fi := app.Index.Files[path]
	tags := make(map[string]map[string]map[string][]string)
	if fi != nil && fi.Tags != nil {
		for cat, groups := range fi.Tags {
			tags[cat] = make(map[string]map[string][]string)
			for group, mods := range groups {
				tags[cat][group] = make(map[string][]string)
				for mod, tagList := range mods {
					tags[cat][group][mod] = append([]string{}, tagList...)
				}
			}
		}
	}

	// Apply deletions - check if this file is in deletion scope
	for _, del := range deletions {
		found := false
		for _, f := range del.Files {
			if f == path {
				found = true
				break
			}
		}
		if !found {
			continue
		}

		app.deleteRefFromTags(tags, del.TagRef)
	}

	// Apply additions - idempotent, skip if tag exists
	for _, add := range additions {
		if tags[add.Category] == nil {
			tags[add.Category] = make(map[string]map[string][]string)
		}
		if tags[add.Category][add.Group] == nil {
			tags[add.Category][add.Group] = make(map[string][]string)
		}
		if add.Tag != "" {
			// Check if tag already exists
			found := false
			for _, t := range tags[add.Category][add.Group][add.Module] {
				if t == add.Tag {
					found = true
					break
				}
			}
			if !found {
				tags[add.Category][add.Group][add.Module] = append(
					tags[add.Category][add.Group][add.Module], add.Tag)
			}
		} else if add.Module != "" {
			// Module without tags - ensure it exists
			if _, ok := tags[add.Category][add.Group][add.Module]; !ok {
				tags[add.Category][add.Group][add.Module] = nil
			}
		}
	}

	// Serialize
	hierarchyLine := serializeTags(tags)

	// Update or insert hierarchy line
	if hierarchyLine == "" {
		if hierarchyLineIdx >= 0 {
			lines = append(lines[:hierarchyLineIdx], lines[hierarchyLineIdx+1:]...)
		}
	} else if hierarchyLineIdx >= 0 {
		lines[hierarchyLineIdx] = hierarchyLine
	} else if packageLineIdx >= 0 {
		// Insert AFTER package line
		lines = slices.Insert(lines, packageLineIdx+1, hierarchyLine)
	}

	// Write back
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return false
	}

	w := bufio.NewWriter(f)
	for i, line := range lines {
		w.WriteString(line)
		// Always add newline between lines, final newline based on original
		if i < len(lines)-1 || hasTrailingNewline {
			w.WriteByte('\n')
		}
	}
	w.Flush()
	f.Close()

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return false
	}

	return true
}

// deleteRefFromTags removes content at the ref's hierarchy level
func (app *AppState) deleteRefFromTags(tags map[string]map[string]map[string][]string, ref TagRef) {
	groups, ok := tags[ref.Category]
	if !ok {
		return
	}

	// Category-level deletion
	if ref.Group == "" {
		delete(tags, ref.Category)
		return
	}

	mods, ok := groups[ref.Group]
	if !ok {
		return
	}

	// Group-level deletion
	if ref.Module == "" {
		delete(groups, ref.Group)
		if len(groups) == 0 {
			delete(tags, ref.Category)
		}
		return
	}

	tagList, ok := mods[ref.Module]
	if !ok {
		return
	}

	// Module-level deletion
	if ref.Tag == "" {
		delete(mods, ref.Module)
		if len(mods) == 0 {
			delete(groups, ref.Group)
			if len(groups) == 0 {
				delete(tags, ref.Category)
			}
		}
		return
	}

	// Tag-level deletion
	newTags := make([]string, 0, len(tagList))
	for _, t := range tagList {
		if t != ref.Tag {
			newTags = append(newTags, t)
		}
	}
	if len(newTags) == 0 {
		delete(mods, ref.Module)
	} else {
		mods[ref.Module] = newTags
	}
	if len(mods) == 0 {
		delete(groups, ref.Group)
	}
	if len(groups) == 0 {
		delete(tags, ref.Category)
	}
}

func serializeTags(tags map[string]map[string]map[string][]string) string {
	if len(tags) == 0 {
		return ""
	}

	cats := make([]string, 0, len(tags))
	for cat := range tags {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	var parts []string
	for _, cat := range cats {
		groups := tags[cat]
		if len(groups) == 0 {
			continue
		}

		// Check for 2-level (category-direct tags only)
		if len(groups) == 1 {
			if directMods, ok := groups[DirectTagsGroup]; ok && len(directMods) == 1 {
				if directTags, ok := directMods[DirectTagsModule]; ok && len(directTags) > 0 {
					sort.Strings(directTags)
					parts = append(parts, fmt.Sprintf("#%s(%s)", cat, strings.Join(directTags, ",")))
					continue
				}
			}
		}

		// Build group names, excluding DirectTagsGroup
		groupNames := make([]string, 0, len(groups))
		for g := range groups {
			if g != DirectTagsGroup {
				groupNames = append(groupNames, g)
			}
		}
		sort.Strings(groupNames)

		var groupParts []string

		// Handle mixed case: direct category tags alongside groups
		if directMods, ok := groups[DirectTagsGroup]; ok {
			if directTags, ok := directMods[DirectTagsModule]; ok && len(directTags) > 0 {
				sort.Strings(directTags)
				groupParts = append(groupParts, fmt.Sprintf("(%s)", strings.Join(directTags, ",")))
			}
		}

		// Handle regular groups
		for _, group := range groupNames {
			mods := groups[group]
			var groupContent []string

			// 3-level: group(tags) — direct tags on group
			if directTags, ok := mods[""]; ok && len(directTags) > 0 {
				sort.Strings(directTags)
				groupContent = append(groupContent, fmt.Sprintf("%s(%s)", group, strings.Join(directTags, ",")))
			}

			// 4-level: group[module(tags)]
			modNames := make([]string, 0, len(mods))
			for m := range mods {
				if m != "" {
					modNames = append(modNames, m)
				}
			}
			sort.Strings(modNames)

			for _, mod := range modNames {
				modTags := mods[mod]
				if len(modTags) == 0 {
					groupContent = append(groupContent, fmt.Sprintf("%s[%s]", group, mod))
				} else {
					sort.Strings(modTags)
					groupContent = append(groupContent, fmt.Sprintf("%s[%s(%s)]", group, mod, strings.Join(modTags, ",")))
				}
			}

			groupParts = append(groupParts, groupContent...)
		}

		if len(groupParts) > 0 {
			parts = append(parts, fmt.Sprintf("#%s{%s}", cat, strings.Join(groupParts, ",")))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return "// @" + HierarchyLinePrefix + ": " + strings.Join(parts, ",")
}

func (app *AppState) saveEditorChanges() {
	if app.Editor == nil || !app.Editor.Dirty() {
		app.CloseEditor()
		return
	}
}

func (app *AppState) renderEditor(r tui.Region) {
	e := app.Editor
	if e == nil || !e.Visible {
		return
	}

	title := fmt.Sprintf("TAG EDITOR (%d files)", len(e.SelectedFiles))
	hint := "Tab:pane  Space:delete  Ctrl+S:save  Esc:cancel"

	content := r.Modal(tui.ModalOpts{
		Title:    title,
		Hint:     hint,
		Border:   tui.LineDouble,
		BorderFg: app.Theme.Border,
		TitleFg:  app.Theme.HeaderFg,
		HintFg:   app.Theme.StatusFg,
		Bg:       app.Theme.Bg,
	})

	mainArea, inputPane := tui.SplitVFixed(content, content.H-4)
	statusBar := inputPane.Sub(0, inputPane.H-1, inputPane.W, 1)
	inputPane = inputPane.Sub(0, 0, inputPane.W, inputPane.H-1)

	leftPane, rightPane := tui.SplitHFixed(mainArea, mainArea.W/3)
	rightPane.VLine(0, tui.LineSingle, app.Theme.Border)
	rightContent := rightPane.Sub(1, 0, rightPane.W-1, rightPane.H)

	app.renderEditorFiles(leftPane)
	app.renderEditorTags(rightContent)
	app.renderEditorInput(inputPane)
	app.renderEditorStatus(statusBar)
}

func (app *AppState) renderEditorFiles(r tui.Region) {
	e := app.Editor

	titleFg := app.Theme.StatusFg
	if e.FocusPane == EditorFocusFiles {
		titleFg = app.Theme.HeaderFg
	}
	r.Text(1, 0, "SELECTED FILES", titleFg, app.Theme.Bg, terminal.AttrBold)

	listArea := r.Sub(0, 1, r.W, r.H-1)
	if len(e.SelectedFiles) == 0 {
		listArea.TextCenter(listArea.H/2, "(no files)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	e.FileState.SetVisible(listArea.H)

	nodes := make([]tui.TreeNode, len(e.SelectedFiles))
	for i, path := range e.SelectedFiles {
		nodes[i] = tui.TreeNode{
			Key:         path,
			Label:       filepath.Base(path),
			Depth:       0,
			Check:       tui.CheckFull,
			CheckFg:     app.Theme.Selected,
			Style:       tui.Style{Fg: app.Theme.FileFg},
			Suffix:      " " + filepath.Dir(path),
			SuffixStyle: tui.Style{Fg: app.Theme.StatusFg, Attr: terminal.AttrDim},
		}
	}

	bg := app.Theme.Bg
	if e.FocusPane == EditorFocusFiles {
		bg = app.Theme.FocusBg
	}

	listArea.Tree(nodes, e.FileState.Cursor, e.FileState.Scroll, tui.TreeOpts{
		CursorBg:  app.Theme.CursorBg,
		DefaultBg: bg,
		IconWidth: 0,
	})
}

func (app *AppState) renderEditorTags(r tui.Region) {
	e := app.Editor

	titleFg := app.Theme.StatusFg
	if e.FocusPane == EditorFocusTags {
		titleFg = app.Theme.HeaderFg
	}
	r.Text(1, 0, "TAGS (space:delete)", titleFg, app.Theme.Bg, terminal.AttrBold)

	treeArea := r.Sub(0, 1, r.W, r.H-1)
	if len(e.TagTree) == 0 {
		treeArea.TextCenter(treeArea.H/2, "(no tags)", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
		return
	}

	e.TagState.SetVisible(treeArea.H)
	e.TagNodes = app.buildEditorTreeNodes()

	bg := app.Theme.Bg
	if e.FocusPane == EditorFocusTags {
		bg = app.Theme.FocusBg
	}

	treeArea.Tree(e.TagNodes, e.TagState.Cursor, e.TagState.Scroll, tui.TreeOpts{
		CursorBg:    app.Theme.CursorBg,
		DefaultBg:   bg,
		IndentWidth: 2,
		IconWidth:   2,
	})
}

func (app *AppState) renderEditorInput(r tui.Region) {
	e := app.Editor

	r.HLine(0, tui.LineSingle, app.Theme.Border)

	titleFg := app.Theme.StatusFg
	if e.FocusPane == EditorFocusRaw {
		titleFg = app.Theme.HeaderFg
	}
	r.Text(1, 1, "ADD TAG (Enter to submit)", titleFg, app.Theme.Bg, terminal.AttrBold)

	// Single line input using Input helper
	r.Input(2, tui.InputOpts{
		Label:    "Tag: ",
		LabelFg:  app.Theme.StatusFg,
		Text:     e.RawInput.Value(),
		Cursor:   e.RawInput.Cursor,
		CursorBg: app.Theme.HeaderFg,
		TextFg:   app.Theme.HeaderFg,
		Bg:       app.Theme.InputBg,
	})
}

func (app *AppState) renderEditorStatus(r tui.Region) {
	e := app.Editor
	r.Fill(app.Theme.Bg)

	adds := len(e.Additions)
	dels := len(e.Deletions)

	if adds > 0 || dels > 0 {
		status := fmt.Sprintf("Pending: +%d tags, -%d tags", adds, dels)
		r.Text(1, 0, status, app.Theme.TagFg, app.Theme.Bg, terminal.AttrNone)
	} else {
		r.Text(1, 0, "No pending changes", app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
	}

	var focusStr string
	switch e.FocusPane {
	case EditorFocusTags:
		focusStr = "[Tags]"
	case EditorFocusRaw:
		focusStr = "[Input]"
	case EditorFocusFiles:
		focusStr = "[Files]"
	}
	r.TextRight(0, focusStr+" ", app.Theme.HintFg, app.Theme.Bg, terminal.AttrNone)
}