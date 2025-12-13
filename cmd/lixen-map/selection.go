package main

import (
	"maps"
	"path/filepath"
	"slices"
	"sort"
)

// ExpandDeps expands package set with transitive local dependencies
func ExpandDeps(selected map[string]bool, index *Index, maxDepth int) map[string]bool {
	result := maps.Clone(selected)
	frontier := slices.Collect(maps.Keys(selected))

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, dir := range frontier {
			if pkg, ok := index.Packages[dir]; ok {
				for _, dep := range pkg.LocalDeps {
					// Find package by name - need to search
					for pkgDir, pkgInfo := range index.Packages {
						if pkgInfo.Name == dep && !result[pkgDir] {
							result[pkgDir] = true
							next = append(next, pkgDir)
						}
					}
				}
			}
		}
		frontier = next
	}

	return result
}

// ComputeOutputFiles generates final deduplicated file list for export
func (app *AppState) ComputeOutputFiles() []string {
	fileSet := make(map[string]bool)

	// Directly selected files
	for path := range app.Selected {
		if app.Index.Files[path] != nil {
			fileSet[path] = true
		}
	}

	// Dependency expansion
	if app.ExpandDeps && len(app.Selected) > 0 {
		selectedDirs := make(map[string]bool)
		for path := range app.Selected {
			dir := filepath.Dir(path)
			dir = filepath.ToSlash(dir)
			if dir == "." {
				if fi, ok := app.Index.Files[path]; ok {
					dir = fi.Package
				}
			}
			selectedDirs[dir] = true
		}

		expandedDirs := ExpandDeps(selectedDirs, app.Index, app.DepthLimit)

		for dir := range expandedDirs {
			if pkg, ok := app.Index.Packages[dir]; ok {
				for _, fi := range pkg.Files {
					fileSet[fi.Path] = true
				}
			}
		}
	}

	// Always include #all files
	for _, fi := range app.Index.Files {
		if fi.IsAll {
			fileSet[fi.Path] = true
		}
	}

	result := slices.Collect(maps.Keys(fileSet))
	sort.Strings(result)
	return result
}

// selectFilesWithTag adds all files with specific tag to selection
func (app *AppState) selectFilesWithTag(cat Category, group, tag string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if tags, ok := fi.TagMap(cat)[group]; ok {
			for _, t := range tags {
				if t == tag {
					if !app.Selected[path] {
						app.Selected[path] = true
						count++
					}
					break
				}
			}
		}
	}
	return count
}

// deselectFilesWithTag removes all files with specific tag from selection
func (app *AppState) deselectFilesWithTag(cat Category, group, tag string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if tags, ok := fi.TagMap(cat)[group]; ok {
			for _, t := range tags {
				if t == tag {
					if app.Selected[path] {
						delete(app.Selected, path)
						count++
					}
					break
				}
			}
		}
	}
	return count
}

// selectFilesWithGroup adds all files in group to selection
func (app *AppState) selectFilesWithGroup(cat Category, group string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
			if !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	}
	return count
}

// deselectFilesWithGroup removes all files in group from selection
func (app *AppState) deselectFilesWithGroup(cat Category, group string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
			if app.Selected[path] {
				delete(app.Selected, path)
				count++
			}
		}
	}
	return count
}

// allFilesWithTagSelected checks if all files with specific tag are selected
func (app *AppState) allFilesWithTagSelected(cat Category, group, tag string) bool {
	for path, fi := range app.Index.Files {
		if tags, ok := fi.TagMap(cat)[group]; ok {
			for _, t := range tags {
				if t == tag {
					if !app.Selected[path] {
						return false
					}
					break
				}
			}
		}
	}
	return true
}

// allFilesWithGroupSelected checks if all files in group are selected
func (app *AppState) allFilesWithGroupSelected(cat Category, group string) bool {
	for path, fi := range app.Index.Files {
		if _, ok := fi.TagMap(cat)[group]; ok {
			if !app.Selected[path] {
				return false
			}
		}
	}
	return true
}