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
	pkgSet := maps.Clone(app.Selected)

	if app.ExpandDeps {
		pkgSet = ExpandDeps(pkgSet, app.Index, app.DepthLimit)
	}

	fileSet := make(map[string]bool)

	// Add files from selected/expanded packages
	for pkgName := range pkgSet {
		if pkg, ok := app.Index.Packages[pkgName]; ok {
			for _, f := range pkg.Files {
				// If keyword filter active, intersect
				if app.KeywordFilter != "" && len(app.KeywordMatches) > 0 {
					if !app.KeywordMatches[f.Path] {
						continue
					}
				}
				fileSet[f.Path] = true
			}
		}
	}

	// Always include #all files
	for _, pkg := range app.Index.Packages {
		for _, f := range pkg.Files {
			if f.IsAll {
				fileSet[f.Path] = true
			}
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

// ToggleSelection toggles selection of current package
func (app *AppState) ToggleSelection() {
	if len(app.PackageList) == 0 {
		return
	}
	name := app.PackageList[app.CursorPos]
	app.Selected[name] = !app.Selected[name]
	if !app.Selected[name] {
		delete(app.Selected, name)
	}
}
