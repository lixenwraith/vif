package main

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
)

// ExpandDeps expands selected packages with their dependencies
func ExpandDeps(selected map[string]bool, index *Index, maxDepth int) map[string]bool {
	result := maps.Clone(selected)
	frontier := slices.Collect(maps.Keys(selected))

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var next []string
		for _, pkg := range frontier {
			if info, ok := index.Packages[pkg]; ok {
				for _, dep := range info.LocalDeps {
					if !result[dep] {
						result[dep] = true
						next = append(next, dep)
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
		// Get packages from selected files
		selectedPkgs := make(map[string]bool)
		for path := range app.Selected {
			if fi, ok := app.Index.Files[path]; ok {
				selectedPkgs[fi.Package] = true
			}
		}

		// Expand dependencies
		expandedPkgs := ExpandDeps(selectedPkgs, app.Index, app.DepthLimit)

		// Add files from expanded packages (respecting filters)
		for pkgName := range expandedPkgs {
			if pkg, ok := app.Index.Packages[pkgName]; ok {
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