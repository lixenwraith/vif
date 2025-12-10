package main

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

// ExpandDeps expands selected packages with their dependencies
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

// ComputeOutputFiles generates the final file list
func (app *AppState) ComputeOutputFiles() []string {
	fileSet := make(map[string]bool)

	// Collect directly selected files
	for path := range app.Selected {
		fi := app.Index.Files[path]
		if fi != nil && app.FileMatchesAllFilters(fi) {
			fileSet[path] = true
		}
	}

	// Dependency expansion
	if app.ExpandDeps && len(app.Selected) > 0 {
		// Get package directories from selected files
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

		// Expand dependencies
		expandedDirs := ExpandDeps(selectedDirs, app.Index, app.DepthLimit)

		// Add files from expanded packages (respecting filters)
		for dir := range expandedDirs {
			if pkg, ok := app.Index.Packages[dir]; ok {
				for _, fi := range pkg.Files {
					if app.FileMatchesAllFilters(fi) {
						fileSet[fi.Path] = true
					}
				}
			}
		}
	}

	// Always include #all files (if they match filters)
	for _, fi := range app.Index.Files {
		if fi.IsAll && app.FileMatchesAllFilters(fi) {
			fileSet[fi.Path] = true
		}
	}

	result := slices.Collect(maps.Keys(fileSet))
	sort.Strings(result)

	return result
}

// WriteOutputFile writes the catalog to file
func WriteOutputFile(path string, files []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, file := range files {
		fmt.Fprintf(w, "./%s\n", file)
	}
	return w.Flush()
}