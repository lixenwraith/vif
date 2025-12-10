package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// SearchKeyword shells to rg for content search
func SearchKeyword(root, pattern string, caseSensitive bool) ([]string, error) {
	args := []string{"--files-with-matches", "-g", "*.go"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	args = append(args, "--", pattern, root)

	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // No matches
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

// FileMatchesTagFilter checks if a file matches current tag filter
func (app *AppState) FileMatchesTagFilter(fi *FileInfo) bool {
	if fi == nil {
		return false
	}

	// No tags selected = match all
	if !app.Filter.HasSelectedTags() {
		return true
	}

	if app.Filter.Mode == FilterOR {
		return app.fileMatchesOR(fi)
	}
	return app.fileMatchesAND(fi)
}

// fileMatchesOR returns true if file has ANY selected tag
func (app *AppState) fileMatchesOR(fi *FileInfo) bool {
	for group, selectedTags := range app.Filter.SelectedTags {
		for tag, selected := range selectedTags {
			if !selected {
				continue
			}
			// Check if file has this tag
			if fileTags, ok := fi.Tags[group]; ok {
				for _, ft := range fileTags {
					if ft == tag {
						return true
					}
				}
			}
		}
	}
	return false
}

// fileMatchesAND returns true if file has at least one selected tag from EACH group with selections
func (app *AppState) fileMatchesAND(fi *FileInfo) bool {
	for group, selectedTags := range app.Filter.SelectedTags {
		// Check if this group has any selections
		hasSelection := false
		for _, selected := range selectedTags {
			if selected {
				hasSelection = true
				break
			}
		}
		if !hasSelection {
			continue
		}

		// File must have at least one selected tag from this group
		matched := false
		if fileTags, ok := fi.Tags[group]; ok {
			for _, ft := range fileTags {
				if selectedTags[ft] {
					matched = true
					break
				}
			}
		}

		if !matched {
			return false
		}
	}
	return true
}

// FileMatchesKeyword checks if file matches keyword filter
func (app *AppState) FileMatchesKeyword(path string) bool {
	if app.Filter.Keyword == "" {
		return true
	}
	return app.Filter.KeywordMatch[path]
}

// FileMatchesAllFilters checks if file passes all active filters
func (app *AppState) FileMatchesAllFilters(fi *FileInfo) bool {
	if fi == nil {
		return false
	}

	// Keyword filter
	if !app.FileMatchesKeyword(fi.Path) {
		return false
	}

	// Tag filter
	if !app.FileMatchesTagFilter(fi) {
		return false
	}

	return true
}

// CountFilteredFiles returns count of files matching current filters
func (app *AppState) CountFilteredFiles() int {
	count := 0
	for _, fi := range app.Index.Files {
		if app.FileMatchesAllFilters(fi) {
			count++
		}
	}
	return count
}