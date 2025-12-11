package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyFilter applies a new filter using current mode
func (app *AppState) ApplyFilter(newPaths []string) {
	newSet := make(map[string]bool, len(newPaths))
	for _, p := range newPaths {
		newSet[p] = true
	}

	if len(app.Filter.FilteredPaths) == 0 {
		// First filter always sets the base, except NOT which needs a base
		if app.Filter.Mode == FilterNOT {
			return // Nothing to subtract from
		}
		app.Filter.FilteredPaths = newSet
	} else {
		switch app.Filter.Mode {
		case FilterOR:
			for p := range newSet {
				app.Filter.FilteredPaths[p] = true
			}
		case FilterAND:
			result := make(map[string]bool)
			for p := range app.Filter.FilteredPaths {
				if newSet[p] {
					result[p] = true
				}
			}
			app.Filter.FilteredPaths = result
		case FilterNOT:
			for p := range newSet {
				delete(app.Filter.FilteredPaths, p)
			}
		case FilterXOR:
			for p := range newSet {
				if app.Filter.FilteredPaths[p] {
					delete(app.Filter.FilteredPaths, p)
				} else {
					app.Filter.FilteredPaths[p] = true
				}
			}
		}
	}

	app.computeFilteredTags()
}

// RemoveFromFilter removes paths from the current filter
func (app *AppState) RemoveFromFilter(paths []string) {
	for _, p := range paths {
		delete(app.Filter.FilteredPaths, p)
	}
	if len(app.Filter.FilteredPaths) == 0 {
		app.ClearFilter()
	} else {
		app.computeFilteredTags()
	}
}

// ClearFilter clears all filter state
func (app *AppState) ClearFilter() {
	app.Filter.FilteredPaths = make(map[string]bool)
	app.Filter.FilteredTags = make(map[string]map[string]bool)
}

// computeFilteredTags derives highlighted tags from FilteredPaths
func (app *AppState) computeFilteredTags() {
	app.Filter.FilteredTags = make(map[string]map[string]bool)

	for path := range app.Filter.FilteredPaths {
		fi := app.Index.Files[path]
		if fi == nil {
			continue
		}
		for group, tags := range fi.Tags {
			if app.Filter.FilteredTags[group] == nil {
				app.Filter.FilteredTags[group] = make(map[string]bool)
			}
			for _, tag := range tags {
				app.Filter.FilteredTags[group][tag] = true
			}
		}
	}
}

// selectFilteredFiles transfers all filtered files to selection
func (app *AppState) selectFilteredFiles() int {
	count := 0
	for path := range app.Filter.FilteredPaths {
		if !app.Selected[path] {
			app.Selected[path] = true
			count++
		}
	}
	return count
}

// applyLeftPaneFilter toggles filter based on left pane cursor position
func (app *AppState) applyLeftPaneFilter() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]
	var paths []string

	if node.IsDir {
		collectFilePaths(node, &paths)
	} else {
		paths = []string{node.Path}
	}

	if len(paths) == 0 {
		return
	}

	// Check if already filtered - toggle behavior
	if app.isPathSetFiltered(paths) {
		app.RemoveFromFilter(paths)
		if node.IsDir {
			app.Message = fmt.Sprintf("unfilter: %s/", node.Path)
		} else {
			app.Message = fmt.Sprintf("unfilter: %s", node.Path)
		}
	} else {
		app.ApplyFilter(paths)
		if node.IsDir {
			app.Message = fmt.Sprintf("filter: %s/ (%d files)", node.Path, len(paths))
		} else {
			app.Message = fmt.Sprintf("filter: %s", node.Path)
		}
	}

	app.RefreshTagFlat()
}

// applyRightPaneFilter toggles filter based on right pane cursor position
func (app *AppState) applyRightPaneFilter() {
	if len(app.TagFlat) == 0 {
		return
	}

	item := app.TagFlat[app.TagCursor]
	var paths []string

	if item.IsGroup {
		for path, fi := range app.Index.Files {
			if _, ok := fi.Tags[item.Group]; ok {
				paths = append(paths, path)
			}
		}
	} else {
		for path, fi := range app.Index.Files {
			if tags, ok := fi.Tags[item.Group]; ok {
				for _, t := range tags {
					if t == item.Tag {
						paths = append(paths, path)
						break
					}
				}
			}
		}
	}

	if len(paths) == 0 {
		return
	}

	// Check if already filtered - toggle behavior
	if app.isPathSetFiltered(paths) {
		app.RemoveFromFilter(paths)
		if item.IsGroup {
			app.Message = fmt.Sprintf("unfilter: #%s", item.Group)
		} else {
			app.Message = fmt.Sprintf("unfilter: #%s{%s}", item.Group, item.Tag)
		}
	} else {
		app.ApplyFilter(paths)
		if item.IsGroup {
			app.Message = fmt.Sprintf("filter: #%s (%d files)", item.Group, len(paths))
		} else {
			app.Message = fmt.Sprintf("filter: #%s{%s} (%d files)", item.Group, item.Tag, len(paths))
		}
	}

	app.RefreshTagFlat()
}

// isPathSetFiltered returns true if all paths in set are currently filtered
func (app *AppState) isPathSetFiltered(paths []string) bool {
	if !app.Filter.HasActiveFilter() || len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !app.Filter.FilteredPaths[p] {
			return false
		}
	}
	return true
}

// collectFilePaths recursively collects file paths under a node
func collectFilePaths(node *TreeNode, paths *[]string) {
	if !node.IsDir {
		*paths = append(*paths, node.Path)
		return
	}
	for _, child := range node.Children {
		collectFilePaths(child, paths)
	}
}

// executeSearch performs pane-aware search
// executeSearch performs search based on active SearchType
func (app *AppState) executeSearch(query string) {
	if query == "" {
		return
	}

	var paths []string
	var label string

	switch app.SearchType {
	case SearchTypeContent:
		paths = searchContentRg(app.Index, query, app.RgAvailable)
		label = "content"
	case SearchTypeTags:
		paths = searchTagsPrefix(app.Index, query)
		label = "tags"
	case SearchTypeGroups:
		paths = searchGroupsPrefix(app.Index, query)
		label = "groups"
	}

	app.Message = fmt.Sprintf("search %s: %q (%d files)", label, query, len(paths))
	app.ApplyFilter(paths)
	app.RefreshTagFlat()
}

// searchWithRipgrep searches using ripgrep (filename, directory, content)
func searchWithRipgrep(root, pattern string) ([]string, error) {
	args := []string{"--files-with-matches", "-g", "*.go", "-i", "--", pattern, root}
	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	lines := strings.Split(raw, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimPrefix(l, "./")
		l = filepath.ToSlash(l)
		result = append(result, l)
	}
	return result, nil
}

// searchPaths searches file paths (fallback when rg unavailable)
func searchPaths(index *Index, pattern string) []string {
	pattern = strings.ToLower(pattern)
	var matches []string
	for path := range index.Files {
		if strings.Contains(strings.ToLower(path), pattern) {
			matches = append(matches, path)
		}
	}
	return matches
}

// searchContentRg searches file content/names using ripgrep or fallback
func searchContentRg(index *Index, query string, rgAvailable bool) []string {
	if rgAvailable {
		matches, err := searchWithRipgrep(".", query)
		if err != nil {
			return nil
		}
		return matches
	}
	return searchPaths(index, query)
}

// searchTagsPrefix finds files with tags matching prefix (case-insensitive)
func searchTagsPrefix(index *Index, query string) []string {
	query = strings.ToLower(query)
	var matches []string
	seen := make(map[string]bool)

	for path, fi := range index.Files {
		for _, tags := range fi.Tags {
			for _, tag := range tags {
				if strings.HasPrefix(strings.ToLower(tag), query) {
					if !seen[path] {
						matches = append(matches, path)
						seen[path] = true
					}
				}
			}
		}
	}
	return matches
}

// searchGroupsPrefix finds files with groups matching prefix (case-insensitive)
func searchGroupsPrefix(index *Index, query string) []string {
	query = strings.ToLower(query)
	var matches []string
	seen := make(map[string]bool)

	for path, fi := range index.Files {
		for group := range fi.Tags {
			if strings.HasPrefix(strings.ToLower(group), query) {
				if !seen[path] {
					matches = append(matches, path)
					seen[path] = true
				}
			}
		}
	}
	return matches
}

// computeTagSelectionState returns selection state for a specific tag
func (app *AppState) computeTagSelectionState(group, tag string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if tags, ok := fi.Tags[group]; ok {
			for _, t := range tags {
				if t == tag {
					total++
					if app.Selected[path] {
						selected++
					}
					break
				}
			}
		}
	}

	if total == 0 || selected == 0 {
		return TagSelectNone
	}
	if selected == total {
		return TagSelectFull
	}
	return TagSelectPartial
}

// computeGroupSelectionState returns selection state for a group
func (app *AppState) computeGroupSelectionState(group string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if _, ok := fi.Tags[group]; ok {
			total++
			if app.Selected[path] {
				selected++
			}
		}
	}

	if total == 0 || selected == 0 {
		return TagSelectNone
	}
	if selected == total {
		return TagSelectFull
	}
	return TagSelectPartial
}

// isGroupFiltered returns true if any tag in group is filtered
func (app *AppState) isGroupFiltered(group string) bool {
	return len(app.Filter.FilteredTags[group]) > 0
}

// isTagFiltered returns true if specific tag is filtered
func (app *AppState) isTagFiltered(group, tag string) bool {
	if tags, ok := app.Filter.FilteredTags[group]; ok {
		return tags[tag]
	}
	return false
}