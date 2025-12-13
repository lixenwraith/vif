package main

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// BuildIndex scans directory tree and builds complete codebase index
func BuildIndex(root string) (*Index, error) {
	modPath := getModulePath()

	index := &Index{
		ModulePath:      modPath,
		Packages:        make(map[string]*PackageInfo),
		Files:           make(map[string]*FileInfo),
		FocusTags:       make(map[string][]string),
		FocusByGroup:    make(map[string][]string),
		FocusByTag:      make(map[string][]string),
		InteractTags:    make(map[string][]string),
		InteractByGroup: make(map[string][]string),
		InteractByTag:   make(map[string][]string),
	}

	focusGroupSet := make(map[string]bool)
	focusTagsByGroup := make(map[string]map[string]bool)
	interactGroupSet := make(map[string]bool)
	interactTagsByGroup := make(map[string]map[string]bool)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

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

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/.") {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)

		fi, err := parseFile(relPath, modPath)
		if err != nil || fi == nil {
			return nil
		}

		index.Files[relPath] = fi

		dir := filepath.Dir(relPath)
		if dir == "." {
			dir = fi.Package
		}
		dir = filepath.ToSlash(dir)

		pkg, ok := index.Packages[dir]
		if !ok {
			pkg = &PackageInfo{
				Name:        fi.Package,
				Dir:         dir,
				Files:       make([]*FileInfo, 0),
				AllFocus:    make(map[string][]string),
				AllInteract: make(map[string][]string),
			}
			index.Packages[dir] = pkg
		}

		pkg.Files = append(pkg.Files, fi)
		if fi.IsAll {
			pkg.HasAll = true
		}

		// Index focus tags
		for group, tags := range fi.Focus {
			focusGroupSet[group] = true
			if focusTagsByGroup[group] == nil {
				focusTagsByGroup[group] = make(map[string]bool)
			}

			// Package-level aggregation
			existing := pkg.AllFocus[group]
			tagSet := make(map[string]bool)
			for _, t := range existing {
				tagSet[t] = true
			}

			for _, t := range tags {
				focusTagsByGroup[group][t] = true
				if !tagSet[t] {
					existing = append(existing, t)
					tagSet[t] = true
				}
				// Index by group:tag
				key := group + ":" + t
				index.FocusByTag[key] = append(index.FocusByTag[key], relPath)
			}
			pkg.AllFocus[group] = existing

			// Index by group
			index.FocusByGroup[group] = append(index.FocusByGroup[group], relPath)
		}

		// Index interact tags
		for group, tags := range fi.Interact {
			interactGroupSet[group] = true
			if interactTagsByGroup[group] == nil {
				interactTagsByGroup[group] = make(map[string]bool)
			}

			existing := pkg.AllInteract[group]
			tagSet := make(map[string]bool)
			for _, t := range existing {
				tagSet[t] = true
			}

			for _, t := range tags {
				interactTagsByGroup[group][t] = true
				if !tagSet[t] {
					existing = append(existing, t)
					tagSet[t] = true
				}
				key := group + ":" + t
				index.InteractByTag[key] = append(index.InteractByTag[key], relPath)
			}
			pkg.AllInteract[group] = existing
			index.InteractByGroup[group] = append(index.InteractByGroup[group], relPath)
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

	// Build sorted focus groups
	for g := range focusGroupSet {
		if g != "all" {
			index.FocusGroups = append(index.FocusGroups, g)
		}
	}
	sort.Strings(index.FocusGroups)

	// Build sorted focus tags per group
	for group, tagSet := range focusTagsByGroup {
		tags := make([]string, 0, len(tagSet))
		for t := range tagSet {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		index.FocusTags[group] = tags
	}

	// Build sorted interact groups
	for g := range interactGroupSet {
		index.InteractGroups = append(index.InteractGroups, g)
	}
	sort.Strings(index.InteractGroups)

	// Build sorted interact tags per group
	for group, tagSet := range interactTagsByGroup {
		tags := make([]string, 0, len(tagSet))
		for t := range tagSet {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		index.InteractTags[group] = tags
	}

	index.ReverseDeps = computeReverseDeps(index)

	return index, nil
}

// ReindexAll rebuilds index from disk and refreshes all views
func (app *AppState) ReindexAll() {
	index, err := BuildIndex(".")
	if err != nil {
		app.Message = fmt.Sprintf("reindex error: %v", err)
		return
	}

	app.Index = index
	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()
	app.RefreshFocusFlat()
	app.RefreshInteractFlat()
	app.Message = fmt.Sprintf("reindexed: %d files", len(index.Files))
}

// parseFile extracts metadata from a single Go source file
func parseFile(path, modPath string) (*FileInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fi := &FileInfo{
		Path:     path,
		Focus:    make(map[string][]string),
		Interact: make(map[string][]string),
		Size:     int64(len(content)),
	}

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
			if err := parseLixenLine(trimmed, fi); err != nil {
				// Skip malformed lines
				continue
			}
		}
	}

	if fi.Package == "" {
		return nil, nil
	}

	// Parse imports
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

// parseLixenLine parses a single @lixen: comment line into FileInfo
func parseLixenLine(line string, fi *FileInfo) error {
	line = strings.TrimPrefix(line, "//")
	line = strings.TrimSpace(line)

	if !strings.HasPrefix(line, "@lixen:") {
		return nil
	}

	// Extract and normalize content (strip all whitespace)
	content := strings.TrimPrefix(line, "@lixen:")
	content = stripWhitespace(content)

	if content == "" {
		return nil
	}

	// Parse blocks: #focus{...},#interact{...}
	return parseBlocks(content, fi)
}

// stripWhitespace removes all whitespace from string
func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// parseBlocks parses "#focus{...},#interact{...}" into FileInfo
func parseBlocks(content string, fi *FileInfo) error {
	// Split by #, filter empty
	for len(content) > 0 {
		if content[0] == ',' {
			content = content[1:]
			continue
		}

		if content[0] != '#' {
			return fmt.Errorf("expected '#', got '%c'", content[0])
		}
		content = content[1:]

		// Find block type
		braceIdx := strings.Index(content, "{")
		if braceIdx == -1 {
			return fmt.Errorf("missing '{' in block")
		}

		blockType := content[:braceIdx]
		content = content[braceIdx+1:]

		// Find matching close brace
		closeIdx := findMatchingBrace(content)
		if closeIdx == -1 {
			return fmt.Errorf("missing '}' in block")
		}

		blockContent := content[:closeIdx]
		content = content[closeIdx+1:]

		// Parse group entries
		groups, err := parseGroupEntries(blockContent)
		if err != nil {
			return err
		}

		switch blockType {
		case "focus":
			for g, tags := range groups {
				if g == "all" && len(tags) > 0 && tags[0] == "*" {
					fi.IsAll = true
					continue
				}
				fi.Focus[g] = append(fi.Focus[g], tags...)
			}
		case "interact":
			for g, tags := range groups {
				fi.Interact[g] = append(fi.Interact[g], tags...)
			}
		default:
			return fmt.Errorf("unknown block type: %s", blockType)
		}
	}

	return nil
}

// findMatchingBrace finds index of matching '}' accounting for nested brackets
func findMatchingBrace(s string) int {
	depth := 1
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseGroupEntries parses "group1[tag1,tag2],group2[tag3]" into map
func parseGroupEntries(content string) (map[string][]string, error) {
	result := make(map[string][]string)

	for len(content) > 0 {
		if content[0] == ',' {
			content = content[1:]
			continue
		}

		// Find group name (up to '[')
		bracketIdx := strings.Index(content, "[")
		if bracketIdx == -1 {
			return nil, fmt.Errorf("missing '[' in group entry")
		}

		groupName := content[:bracketIdx]
		content = content[bracketIdx+1:]

		// Find closing bracket
		closeIdx := strings.Index(content, "]")
		if closeIdx == -1 {
			return nil, fmt.Errorf("missing ']' in group entry")
		}

		tagList := content[:closeIdx]
		content = content[closeIdx+1:]

		// Parse tags
		tags := strings.Split(tagList, ",")
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t != "" {
				result[groupName] = append(result[groupName], t)
			}
		}
	}

	return result, nil
}

// getModulePath reads module path from go.mod file
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