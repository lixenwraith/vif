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
	app.Filter.FilteredFocusTags = make(map[string]map[string]map[string]bool)
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

// applyTreePaneFilter toggles filter for item at tree cursor
func (app *AppState) applyTreePaneFilter() {
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

	app.RefreshFocusFlat()
	app.RefreshInteractFlat()
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

// computeTagSelectionState returns selection coverage for a specific tag
func (app *AppState) computeTagSelectionState(cat Category, group, module, tag string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if mods, ok := fi.TagMap(cat)[group]; ok {
			if tags, ok := mods[module]; ok {
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
	}

	if total == 0 || selected == 0 {
		return TagSelectNone
	}
	if selected == total {
		return TagSelectFull
	}
	return TagSelectPartial
}

// computeModuleSelectionState returns selection coverage for a module
func (app *AppState) computeModuleSelectionState(cat Category, group, module string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if mods, ok := fi.TagMap(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				total++
				if app.Selected[path] {
					selected++
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
func (app *AppState) computeGroupSelectionState(cat Category, group string) TagSelectionState {
	total := 0
	selected := 0

	for path, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
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

// isGroupFiltered returns true if any module/tag in group matches filter
func (app *AppState) isGroupFiltered(cat Category, group string) bool {
	filteredTags := app.Filter.FilteredTags(cat)
	if mods, ok := filteredTags[group]; ok {
		for _, tags := range mods {
			if len(tags) > 0 {
				return true
			}
		}
	}
	return false
}

// isModuleFiltered returns true if any tag in module matches filter
func (app *AppState) isModuleFiltered(cat Category, group, module string) bool {
	filteredTags := app.Filter.FilteredTags(cat)
	if mods, ok := filteredTags[group]; ok {
		if tags, ok := mods[module]; ok {
			return len(tags) > 0
		}
	}
	return false
}

// isTagFiltered returns true if specific tag is filtered
func (app *AppState) isTagFiltered(cat Category, group, module, tag string) bool {
	filteredTags := app.Filter.FilteredTags(cat)
	if mods, ok := filteredTags[group]; ok {
		if tags, ok := mods[module]; ok {
			return tags[tag]
		}
	}
	return false
}

// computeFilteredTags derives tag highlights from filtered file paths
func (app *AppState) computeFilteredTags() {
	app.Filter.FilteredFocusTags = make(map[string]map[string]map[string]bool)
	app.Filter.FilteredInteractTags = make(map[string]map[string]map[string]bool)

	for path := range app.Filter.FilteredPaths {
		fi := app.Index.Files[path]
		if fi == nil {
			continue
		}

		// Focus tags
		for group, mods := range fi.Focus {
			if app.Filter.FilteredFocusTags[group] == nil {
				app.Filter.FilteredFocusTags[group] = make(map[string]map[string]bool)
			}
			for module, tags := range mods {
				if app.Filter.FilteredFocusTags[group][module] == nil {
					app.Filter.FilteredFocusTags[group][module] = make(map[string]bool)
				}
				for _, tag := range tags {
					app.Filter.FilteredFocusTags[group][module][tag] = true
				}
			}
		}

		// Interact tags
		for group, mods := range fi.Interact {
			if app.Filter.FilteredInteractTags[group] == nil {
				app.Filter.FilteredInteractTags[group] = make(map[string]map[string]bool)
			}
			for module, tags := range mods {
				if app.Filter.FilteredInteractTags[group][module] == nil {
					app.Filter.FilteredInteractTags[group][module] = make(map[string]bool)
				}
				for _, tag := range tags {
					app.Filter.FilteredInteractTags[group][module][tag] = true
				}
			}
		}
	}
}

// applyFocusPaneFilter toggles filter for item at focus cursor
func (app *AppState) applyFocusPaneFilter() {
	if len(app.TagFlat) == 0 {
		return
	}

	item := app.TagFlat[app.TagCursor]
	var paths []string

	switch item.Type {
	case TagItemTypeGroup:
		for path, fi := range app.Index.Files {
			if _, ok := fi.Focus[item.Group]; ok {
				paths = append(paths, path)
			}
		}
	case TagItemTypeModule:
		for path, fi := range app.Index.Files {
			if mods, ok := fi.Focus[item.Group]; ok {
				if _, ok := mods[item.Module]; ok {
					paths = append(paths, path)
				}
			}
		}
	case TagItemTypeTag:
		for path, fi := range app.Index.Files {
			if mods, ok := fi.Focus[item.Group]; ok {
				if tags, ok := mods[item.Module]; ok {
					for _, t := range tags {
						if t == item.Tag {
							paths = append(paths, path)
							break
						}
					}
				}
			}
		}
	}

	if len(paths) == 0 {
		return
	}

	if app.isPathSetFiltered(paths) {
		app.RemoveFromFilter(paths)
		app.Message = fmt.Sprintf("unfilter: %s", formatTagItemLabel(item))
	} else {
		app.ApplyFilter(paths)
		app.Message = fmt.Sprintf("filter: %s (%d files)", formatTagItemLabel(item), len(paths))
	}

	app.RefreshFocusFlat()
	app.RefreshInteractFlat()
}

// applyInteractPaneFilter toggles filter for item at interact cursor
func (app *AppState) applyInteractPaneFilter() {
	if len(app.InteractFlat) == 0 {
		return
	}

	item := app.InteractFlat[app.InteractCursor]
	var paths []string

	switch item.Type {
	case TagItemTypeGroup:
		for path, fi := range app.Index.Files {
			if _, ok := fi.Interact[item.Group]; ok {
				paths = append(paths, path)
			}
		}
	case TagItemTypeModule:
		for path, fi := range app.Index.Files {
			if mods, ok := fi.Interact[item.Group]; ok {
				if _, ok := mods[item.Module]; ok {
					paths = append(paths, path)
				}
			}
		}
	case TagItemTypeTag:
		for path, fi := range app.Index.Files {
			if mods, ok := fi.Interact[item.Group]; ok {
				if tags, ok := mods[item.Module]; ok {
					for _, t := range tags {
						if t == item.Tag {
							paths = append(paths, path)
							break
						}
					}
				}
			}
		}
	}

	if len(paths) == 0 {
		return
	}

	if app.isPathSetFiltered(paths) {
		app.RemoveFromFilter(paths)
		app.Message = fmt.Sprintf("unfilter: %s", formatTagItemLabel(item))
	} else {
		app.ApplyFilter(paths)
		app.Message = fmt.Sprintf("filter: %s (%d files)", formatTagItemLabel(item), len(paths))
	}

	app.RefreshFocusFlat()
	app.RefreshInteractFlat()
}

// formatTagItemLabel returns display label for a TagItem
func formatTagItemLabel(item TagItem) string {
	switch item.Type {
	case TagItemTypeGroup:
		return "#" + item.Group
	case TagItemTypeModule:
		return fmt.Sprintf("#%s[%s]", item.Group, item.Module)
	case TagItemTypeTag:
		if item.Module == DirectTagsModule {
			return fmt.Sprintf("#%s(%s)", item.Group, item.Tag)
		}
		return fmt.Sprintf("#%s[%s(%s)]", item.Group, item.Module, item.Tag)
	}
	return ""
}

// executeSearch performs content search and applies filter
func (app *AppState) executeSearch(query string) {
	if query == "" {
		return
	}

	paths := searchContentRg(app.Index, query, app.RgAvailable)
	app.Message = fmt.Sprintf("filter content: %q (%d files)", query, len(paths))
	app.ApplyFilter(paths)
	app.RefreshFocusFlat()
	app.RefreshInteractFlat()
}