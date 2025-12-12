package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ApplyFilter applies paths to current filter using active mode
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

// RemoveFromFilter removes specified paths from active filter
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

// ClearFilter resets all filter state to empty
func (app *AppState) ClearFilter() {
	app.Filter.FilteredPaths = make(map[string]bool)
	app.Filter.FilteredFocusTags = make(map[string]map[string]bool)
}

// computeFilteredTags derives tag highlights from filtered file paths
func (app *AppState) computeFilteredTags() {
	app.Filter.FilteredFocusTags = make(map[string]map[string]bool)
	app.Filter.FilteredInteractTags = make(map[string]map[string]bool)

	for path := range app.Filter.FilteredPaths {
		fi := app.Index.Files[path]
		if fi == nil {
			continue
		}

		// Focus tags
		for group, tags := range fi.Focus {
			if app.Filter.FilteredFocusTags[group] == nil {
				app.Filter.FilteredFocusTags[group] = make(map[string]bool)
			}
			for _, tag := range tags {
				app.Filter.FilteredFocusTags[group][tag] = true
			}
		}

		// Interact tags
		for group, tags := range fi.Interact {
			if app.Filter.FilteredInteractTags[group] == nil {
				app.Filter.FilteredInteractTags[group] = make(map[string]bool)
			}
			for _, tag := range tags {
				app.Filter.FilteredInteractTags[group][tag] = true
			}
		}
	}
}

// selectFilteredFiles transfers all filtered paths to selection set
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

// applyLeftPaneFilter toggles filter for item at tree cursor
func (app *AppState) applyLeftPaneFilter() {
	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]
	var paths []string

	if node.IsDir {
		collectFiles(node, &paths)
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

// applyRightPaneFilter toggles filter for item at tag cursor
func (app *AppState) applyRightPaneFilter() {
	if len(app.TagFlat) == 0 {
		return
	}

	item := app.TagFlat[app.TagCursor]
	var paths []string

	if item.IsGroup {
		for path, fi := range app.Index.Files {
			if _, ok := fi.Focus[item.Group]; ok {
				paths = append(paths, path)
			}
		}
	} else {
		for path, fi := range app.Index.Files {
			if tags, ok := fi.Focus[item.Group]; ok {
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

// isPathSetFiltered checks if all paths in set are currently filtered
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

// executeSearch performs search using active SearchType and applies filter
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

// searchWithRipgrep executes ripgrep for content search
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

// searchPaths searches file paths by substring (ripgrep fallback)
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

// searchContentRg dispatches content search to ripgrep or fallback
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

// searchTagsPrefix finds files with tags matching query prefix
func searchTagsPrefix(index *Index, query string) []string {
	query = strings.ToLower(query)
	var matches []string
	seen := make(map[string]bool)

	for path, fi := range index.Files {
		for _, tags := range fi.Focus {
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

// computeTagSelectionState returns selection coverage for a specific tag
func searchGroupsPrefix(index *Index, query string) []string {
	query = strings.ToLower(query)
	var matches []string
	seen := make(map[string]bool)

	for path, fi := range index.Files {
		for group := range fi.Focus {
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

// computeGroupSelectionState returns selection coverage for a group
func (app *AppState) computeTagSelectionState(group, tag string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if tags, ok := fi.Focus[group]; ok {
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

// computeGroupSelectionState returns selection coverage for a group
func (app *AppState) computeGroupSelectionState(group string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if _, ok := fi.Focus[group]; ok {
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

// isGroupFiltered returns true if any tag in group matches filter
func (app *AppState) isGroupFiltered(group string) bool {
	return len(app.Filter.FilteredFocusTags[group]) > 0
}

// isTagFiltered returns true if specific tag is filtered
func (app *AppState) isTagFiltered(group, tag string) bool {
	if tags, ok := app.Filter.FilteredFocusTags[group]; ok {
		return tags[tag]
	}
	return false
}