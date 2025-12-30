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

// computeOutputStats calculates file counts and sizes for output
// Returns: total files, dep-only files, total size, dep-only size
func (app *AppState) computeOutputStats() (totalFiles, depFiles int, totalSize, depSize int64) {
	outputFiles := app.ComputeOutputFiles()
	totalFiles = len(outputFiles)

	// Build set of directly selected + #all files
	directSet := make(map[string]bool)
	for path := range app.Selected {
		directSet[path] = true
	}
	for path, fi := range app.Index.Files {
		if fi.IsAll {
			directSet[path] = true
		}
	}

	for _, path := range outputFiles {
		fi := app.Index.Files[path]
		if fi == nil {
			continue
		}
		totalSize += fi.Size

		// Count as dep if in output but not directly selected/#all
		if !directSet[path] {
			depFiles++
			depSize += fi.Size
		}
	}

	return
}

// selectFilesWithTag adds all files with specific tag to selection
func (app *AppState) selectFilesWithTag(cat, group, module, tag string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if tags, ok := mods[module]; ok {
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
	}
	return count
}

// deselectFilesWithTag removes all files with specific tag from selection
func (app *AppState) deselectFilesWithTag(cat, group, module, tag string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if tags, ok := mods[module]; ok {
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
	}
	return count
}

// selectFilesWithModule adds all files in module to selection
func (app *AppState) selectFilesWithModule(cat, group, module string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				if !app.Selected[path] {
					app.Selected[path] = true
					count++
				}
			}
		}
	}
	return count
}

// deselectFilesWithModule removes all files in module from selection
func (app *AppState) deselectFilesWithModule(cat, group, module string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				if app.Selected[path] {
					delete(app.Selected, path)
					count++
				}
			}
		}
	}
	return count
}

// selectFilesWithGroup adds all files in group to selection
func (app *AppState) selectFilesWithGroup(cat, group string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if _, ok := fi.CategoryTags(cat)[group]; ok {
			if !app.Selected[path] {
				app.Selected[path] = true
				count++
			}
		}
	}
	return count
}

// deselectFilesWithGroup removes all files in group from selection
func (app *AppState) deselectFilesWithGroup(cat, group string) int {
	count := 0
	for path, fi := range app.Index.Files {
		if _, ok := fi.CategoryTags(cat)[group]; ok {
			if app.Selected[path] {
				delete(app.Selected, path)
				count++
			}
		}
	}
	return count
}

// allFilesWithTagSelected checks if all files with specific tag are selected
func (app *AppState) allFilesWithTagSelected(cat, group, module, tag string) bool {
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if tags, ok := mods[module]; ok {
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
	}
	return true
}

// allFilesWithModuleSelected checks if all files in module are selected
func (app *AppState) allFilesWithModuleSelected(cat, group, module string) bool {
	for path, fi := range app.Index.Files {
		if mods, ok := fi.CategoryTags(cat)[group]; ok {
			if _, ok := mods[module]; ok {
				if !app.Selected[path] {
					return false
				}
			}
		}
	}
	return true
}

// allFilesWithGroupSelected checks if all files in group are selected
func (app *AppState) allFilesWithGroupSelected(cat, group string) bool {
	for path, fi := range app.Index.Files {
		if _, ok := fi.CategoryTags(cat)[group]; ok {
			if !app.Selected[path] {
				return false
			}
		}
	}
	return true
}