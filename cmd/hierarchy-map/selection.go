package main

import (
	"maps"
	"slices"
	"sort"
	"strings"
)

// ExpandDepsFileLevel expands selected files with file-level granularity
// Returns map of file paths that should be included as dependencies
func ExpandDepsFileLevel(selectedFiles map[string]bool, index *Index, cache map[string]*DependencyAnalysis, maxDepth int) map[string]bool {
	result := make(map[string]bool)
	visited := make(map[string]bool)

	// Queue: (filePath, currentDepth)
	type item struct {
		path  string
		depth int
	}
	queue := make([]item, 0, len(selectedFiles))

	for path := range selectedFiles {
		queue = append(queue, item{path, 0})
		visited[path] = true
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current.depth >= maxDepth {
			continue
		}

		fi := index.Files[current.path]
		if fi == nil {
			continue
		}

		// GetComponent or compute analysis
		analysis := cache[current.path]
		if analysis == nil {
			a, err := AnalyzeFileDependencies(current.path, index.ModulePath)
			if err == nil {
				analysis = a
				cache[current.path] = a
			}
		}

		if analysis != nil {
			// Process symbol usage
			for importPath, symbols := range analysis.UsedSymbols {
				pkgDir := importPathToDir(importPath, index.ModulePath)
				pkg := index.Packages[pkgDir]
				if pkg == nil {
					continue
				}

				for _, sym := range symbols {
					if filePath, ok := pkg.SymbolFiles[sym]; ok {
						if !visited[filePath] && !selectedFiles[filePath] {
							result[filePath] = true
							visited[filePath] = true
							queue = append(queue, item{filePath, current.depth + 1})
						}
					}
				}
			}
		}

		// Handle blank imports: include file with init() if found
		for _, blankPkg := range fi.BlankImports {
			pkg := index.Packages[blankPkg]
			if pkg == nil {
				continue
			}

			for _, pkgFile := range pkg.Files {
				if pkgFile.HasInit {
					if !visited[pkgFile.Path] && !selectedFiles[pkgFile.Path] {
						result[pkgFile.Path] = true
						visited[pkgFile.Path] = true
						queue = append(queue, item{pkgFile.Path, current.depth + 1})
					}
					break // Only first init file per package
				}
			}
		}
	}

	return result
}

// importPathToDir converts full import path to package directory
func importPathToDir(importPath, modPath string) string {
	if importPath == modPath {
		return "."
	}
	if strings.HasPrefix(importPath, modPath+"/") {
		return strings.TrimPrefix(importPath, modPath+"/")
	}
	return ""
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

	// File-level dependency expansion
	if app.ExpandDeps && len(app.Selected) > 0 {
		depFiles := ExpandDepsFileLevel(app.Selected, app.Index, app.DepAnalysisCache, app.DepthLimit)
		for path := range depFiles {
			fileSet[path] = true
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

		// CountEntities as dep if in output but not directly selected/#all
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