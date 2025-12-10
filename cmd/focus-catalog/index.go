package main

import (
	"bufio"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildIndex scans the codebase and builds the index
func BuildIndex(root string) (*Index, error) {
	modPath := getModulePath()

	index := &Index{
		ModulePath: modPath,
		Packages:   make(map[string]*PackageInfo),
		Files:      make(map[string]*FileInfo),
	}

	groupSet := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Skip directories
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "testdata" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Only .go files, skip tests
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/.") {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)

		fi, err := parseFile(relPath, modPath)
		if err != nil {
			return nil // skip parse errors
		}
		if fi == nil {
			return nil
		}

		index.Files[relPath] = fi

		// Add to package
		pkg, ok := index.Packages[fi.Package]
		if !ok {
			dir := filepath.Dir(relPath)
			if dir == "." {
				dir = fi.Package
			}
			pkg = &PackageInfo{
				Name:    fi.Package,
				Dir:     dir,
				Files:   make([]*FileInfo, 0),
				AllTags: make(map[string][]string),
			}
			index.Packages[fi.Package] = pkg
		}

		pkg.Files = append(pkg.Files, fi)
		if fi.IsAll {
			pkg.HasAll = true
		}

		// Merge tags
		for group, tags := range fi.Tags {
			groupSet[group] = true
			existing := pkg.AllTags[group]
			tagSet := make(map[string]bool)
			for _, t := range existing {
				tagSet[t] = true
			}
			for _, t := range tags {
				if !tagSet[t] {
					existing = append(existing, t)
					tagSet[t] = true
				}
			}
			pkg.AllTags[group] = existing
		}

		// Merge imports
		depSet := make(map[string]bool)
		for _, d := range pkg.LocalDeps {
			depSet[d] = true
		}
		for _, imp := range fi.Imports {
			if !depSet[imp] {
				pkg.LocalDeps = append(pkg.LocalDeps, imp)
				depSet[imp] = true
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Build sorted groups list
	for g := range groupSet {
		if g != "all" {
			index.Groups = append(index.Groups, g)
		}
	}
	sort.Strings(index.Groups)

	return index, nil
}

// parseFile extracts info from a single Go file
func parseFile(path, modPath string) (*FileInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fi := &FileInfo{
		Path: path,
		Tags: make(map[string][]string),
	}

	// Scan for package and @focus tags
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "package ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				fi.Package = parts[1]
			}
			break
		}

		if strings.HasPrefix(trimmed, "//") {
			tags, isAll, ok := parseTagLine(trimmed)
			if ok {
				for group, t := range tags {
					fi.Tags[group] = append(fi.Tags[group], t...)
				}
				if isAll {
					fi.IsAll = true
				}
			}
		}
	}

	if fi.Package == "" {
		return nil, nil
	}

	// Parse imports from already-read content
	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, path, content, parser.ImportsOnly)
	if err != nil {
		return fi, nil
	}

	for _, imp := range astFile.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(impPath, modPath+"/") {
			localPkg := strings.TrimPrefix(impPath, modPath+"/")
			parts := strings.Split(localPkg, "/")
			fi.Imports = append(fi.Imports, parts[len(parts)-1])
		}
	}

	return fi, nil
}

// parseTagLine parses a @focus comment line
// Returns tags map, isAll flag, and ok
func parseTagLine(line string) (map[string][]string, bool, bool) {
	line = strings.TrimPrefix(line, "//")
	line = strings.TrimSpace(line)

	if !strings.HasPrefix(line, "@focus:") {
		return nil, false, false
	}

	line = strings.TrimPrefix(line, "@focus:")
	line = strings.TrimSpace(line)

	result := make(map[string][]string)
	isAll := false

	// Parse #group { tag, tag } patterns
	for len(line) > 0 {
		// Find next #
		idx := strings.Index(line, "#")
		if idx == -1 {
			break
		}
		line = line[idx+1:]

		// Find group name (until space or {)
		endIdx := strings.IndexAny(line, " \t{")
		var groupName string
		if endIdx == -1 {
			groupName = line
			line = ""
		} else {
			groupName = line[:endIdx]
			line = line[endIdx:]
		}

		groupName = strings.TrimSpace(groupName)
		if groupName == "" {
			continue
		}

		if groupName == "all" {
			isAll = true
			continue
		}

		// Find tags in braces
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			result[groupName] = []string{}
			continue
		}

		line = line[1:] // skip {
		endBrace := strings.Index(line, "}")
		if endBrace == -1 {
			break
		}

		tagsStr := line[:endBrace]
		line = line[endBrace+1:]

		// Parse comma-separated tags
		tags := strings.Split(tagsStr, ",")
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				result[groupName] = append(result[groupName], t)
			}
		}
	}

	return result, isAll, true
}

// getModulePath reads module path from go.mod
func getModulePath() string {
	f, err := os.Open("go.mod")
	if err != nil {
		return defaultModulePath
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}

	return defaultModulePath
}
