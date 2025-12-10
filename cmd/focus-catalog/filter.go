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

// SearchContent shells to rg for content search
func SearchContent(root, pattern string, caseSensitive bool) ([]string, error) {
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

// SearchMeta searches files by metadata (path, package, tags, groups)
func SearchMeta(index *Index, pattern string, caseSensitive bool) []string {
	if !caseSensitive {
		pattern = strings.ToLower(pattern)
	}

	var matches []string
	for path, fi := range index.Files {
		if fileMatchesPattern(path, fi, pattern, caseSensitive) {
			matches = append(matches, path)
		}
	}
	return matches
}

// fileMatchesPattern checks if file metadata contains pattern
func fileMatchesPattern(path string, fi *FileInfo, pattern string, caseSensitive bool) bool {
	// Check path
	checkPath := path
	if !caseSensitive {
		checkPath = strings.ToLower(path)
	}
	if strings.Contains(checkPath, pattern) {
		return true
	}

	// Check package name
	checkPkg := fi.Package
	if !caseSensitive {
		checkPkg = strings.ToLower(fi.Package)
	}
	if strings.Contains(checkPkg, pattern) {
		return true
	}

	// Check groups and tags
	for group, tags := range fi.Tags {
		checkGroup := group
		if !caseSensitive {
			checkGroup = strings.ToLower(group)
		}
		if strings.Contains(checkGroup, pattern) {
			return true
		}
		for _, tag := range tags {
			checkTag := tag
			if !caseSensitive {
				checkTag = strings.ToLower(tag)
			}
			if strings.Contains(checkTag, pattern) {
				return true
			}
		}
	}

	return false
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