package main

import (
	"bufio"
	"fmt"
	"go/ast"
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
		ModulePath: modPath,
		Packages:   make(map[string]*PackageInfo),
		Files:      make(map[string]*FileInfo),
		Categories: make(map[string]*CategoryIndex),
	}

	// Temporary aggregation structures per category
	categoryGroupSets := make(map[string]map[string]bool)
	categoryModuleSets := make(map[string]map[string]map[string]bool)
	categoryTagSets := make(map[string]map[string]map[string]map[string]bool)

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

		// Strict directory-based keys. Root is "."
		dir := filepath.Dir(relPath)
		dir = filepath.ToSlash(dir)
		// filepath.Dir returns "." for root files, which is what we want

		pkg, ok := index.Packages[dir]
		if !ok {
			pkg = &PackageInfo{
				Name:  fi.Package,
				Dir:   dir,
				Files: make([]*FileInfo, 0),
			}
			index.Packages[dir] = pkg
		}

		pkg.Files = append(pkg.Files, fi)
		if fi.IsAll {
			pkg.HasAll = true
		}

		// Index tags per category
		for category, groups := range fi.Tags {
			if categoryGroupSets[category] == nil {
				categoryGroupSets[category] = make(map[string]bool)
			}
			if categoryModuleSets[category] == nil {
				categoryModuleSets[category] = make(map[string]map[string]bool)
			}
			if categoryTagSets[category] == nil {
				categoryTagSets[category] = make(map[string]map[string]map[string]bool)
			}
			if index.Categories[category] == nil {
				index.Categories[category] = NewCategoryIndex()
			}

			catIdx := index.Categories[category]

			for group, modules := range groups {
				categoryGroupSets[category][group] = true

				if categoryModuleSets[category][group] == nil {
					categoryModuleSets[category][group] = make(map[string]bool)
				}
				if categoryTagSets[category][group] == nil {
					categoryTagSets[category][group] = make(map[string]map[string]bool)
				}

				catIdx.ByGroup[group] = append(catIdx.ByGroup[group], relPath)

				for module, tags := range modules {
					if module != DirectTagsModule {
						categoryModuleSets[category][group][module] = true
						moduleKey := group + "." + module
						catIdx.ByModule[moduleKey] = append(catIdx.ByModule[moduleKey], relPath)
					}

					if categoryTagSets[category][group][module] == nil {
						categoryTagSets[category][group][module] = make(map[string]bool)
					}

					for _, tag := range tags {
						categoryTagSets[category][group][module][tag] = true
						var tagKey string
						if module == DirectTagsModule {
							tagKey = group + ".." + tag
						} else {
							tagKey = group + "." + module + "." + tag
						}
						catIdx.ByTag[tagKey] = append(catIdx.ByTag[tagKey], relPath)
					}
				}
			}
		}

		// Merge imports for package-level forward deps (optional, but kept for consistency)
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

	// Build sorted structures for each category
	for category, groupSet := range categoryGroupSets {
		catIdx := index.Categories[category]

		for g := range groupSet {
			if g != "all" {
				catIdx.Groups = append(catIdx.Groups, g)
			}
		}
		sort.Strings(catIdx.Groups)

		for group, modSet := range categoryModuleSets[category] {
			mods := make([]string, 0, len(modSet))
			for m := range modSet {
				mods = append(mods, m)
			}
			sort.Strings(mods)
			catIdx.Modules[group] = mods
		}

		for group, modMap := range categoryTagSets[category] {
			if catIdx.Tags[group] == nil {
				catIdx.Tags[group] = make(map[string][]string)
			}
			for module, tagSet := range modMap {
				tags := make([]string, 0, len(tagSet))
				for t := range tagSet {
					tags = append(tags, t)
				}
				sort.Strings(tags)
				catIdx.Tags[group][module] = tags
			}
		}
	}

	for cat := range index.Categories {
		index.CategoryNames = append(index.CategoryNames, cat)
	}
	sort.Strings(index.CategoryNames)

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
	app.CategoryNames = index.CategoryNames

	if app.CurrentCategory == "" || !index.HasCategory(app.CurrentCategory) {
		if len(index.CategoryNames) > 0 {
			app.CurrentCategory = index.CategoryNames[0]
		} else {
			app.CurrentCategory = ""
		}
	}

	if app.CategoryUI == nil {
		app.CategoryUI = make(map[string]*CategoryUIState)
	}
	for _, cat := range index.CategoryNames {
		if app.CategoryUI[cat] == nil {
			app.CategoryUI[cat] = NewCategoryUIState()
		}
	}

	app.TreeRoot = BuildTree(index)
	app.RefreshTreeFlat()
	app.RefreshLixenFlat()
	app.Message = fmt.Sprintf("reindexed: %d files, %d categories", len(index.Files), len(index.CategoryNames))
}

// parseFile extracts metadata from a single Go source file
func parseFile(path, modPath string) (*FileInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	fi := &FileInfo{
		Path: path,
		Tags: make(map[string]map[string]map[string][]string),
		Size: int64(len(content)),
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
				continue
			}
		}
	}

	if fi.Package == "" {
		return nil, nil
	}

	fset := token.NewFileSet()
	// CHANGED: Parse full AST to extract definitions, not just imports
	// Note: ParseFile is fast enough for individual files.
	astFile, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return fi, nil
	}

	// Extract definitions
	for _, decl := range astFile.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						fi.Definitions = append(fi.Definitions, s.Name.Name)
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if name.IsExported() {
							fi.Definitions = append(fi.Definitions, name.Name)
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Name.IsExported() && d.Recv == nil { // Only package-level funcs, methods are tied to types
				fi.Definitions = append(fi.Definitions, d.Name.Name)
			}
		}
	}

	// Extract imports
	for _, imp := range astFile.Imports {
		if imp.Path == nil {
			continue
		}
		impPath := strings.Trim(imp.Path.Value, `"`)

		// Robust module path stripping
		if impPath == modPath {
			// Import points to module root
			fi.Imports = append(fi.Imports, ".")
		} else if strings.HasPrefix(impPath, modPath+"/") {
			// Import points to submodule: strip "module/path/" -> "path"
			localPkg := strings.TrimPrefix(impPath, modPath+"/")
			fi.Imports = append(fi.Imports, localPkg)
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

	content := strings.TrimPrefix(line, "@lixen:")
	content = stripWhitespace(content)

	if content == "" {
		return nil
	}

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

// parseBlocks parses "#category{...},#category2{...}" into FileInfo
// Categories are dynamically discovered from the content
func parseBlocks(content string, fi *FileInfo) error {
	for len(content) > 0 {
		if content[0] == ',' {
			content = content[1:]
			continue
		}

		if content[0] != '#' {
			return fmt.Errorf("expected '#', got '%c'", content[0])
		}
		content = content[1:]

		braceIdx := strings.Index(content, "{")
		if braceIdx == -1 {
			return fmt.Errorf("missing '{' in block")
		}

		category := content[:braceIdx]
		content = content[braceIdx+1:]

		closeIdx := findMatchingBrace(content)
		if closeIdx == -1 {
			return fmt.Errorf("missing '}' in block")
		}

		blockContent := content[:closeIdx]
		content = content[closeIdx+1:]

		groups, err := parseGroupEntries(blockContent)
		if err != nil {
			return err
		}

		if fi.Tags[category] == nil {
			fi.Tags[category] = make(map[string]map[string][]string)
		}

		for g, modules := range groups {
			if g == "all" {
				if mods, ok := modules[DirectTagsModule]; ok && len(mods) > 0 && mods[0] == "*" {
					fi.IsAll = true
					continue
				}
			}

			if fi.Tags[category][g] == nil {
				fi.Tags[category][g] = make(map[string][]string)
			}
			for mod, tags := range modules {
				fi.Tags[category][g][mod] = append(fi.Tags[category][g][mod], tags...)
			}
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

// parseGroupEntries parses group entries supporting both formats:
// - 2-level: "group(tag1,tag2)" → group with direct tags
// - 3-level: "group[mod1(tag1),mod2]" → group with modules
// Returns: group → module → tags (module="" for 2-level direct tags)
func parseGroupEntries(content string) (map[string]map[string][]string, error) {
	result := make(map[string]map[string][]string)

	for len(content) > 0 {
		if content[0] == ',' {
			content = content[1:]
			continue
		}

		groupEnd := -1
		var format byte
		for i := 0; i < len(content); i++ {
			if content[i] == '[' || content[i] == '(' {
				groupEnd = i
				format = content[i]
				break
			}
		}
		if groupEnd == -1 {
			return nil, fmt.Errorf("missing '[' or '(' in group entry")
		}

		groupName := content[:groupEnd]
		if groupName == "" {
			return nil, fmt.Errorf("empty group name")
		}
		content = content[groupEnd+1:]

		if result[groupName] == nil {
			result[groupName] = make(map[string][]string)
		}

		if format == '(' {
			closeIdx := strings.Index(content, ")")
			if closeIdx == -1 {
				return nil, fmt.Errorf("missing ')' in group entry")
			}
			tagList := content[:closeIdx]
			content = content[closeIdx+1:]

			tags := parseTagList(tagList)
			if len(tags) == 0 {
				return nil, fmt.Errorf("empty tag list in group '%s'", groupName)
			}
			result[groupName][DirectTagsModule] = tags
		} else {
			closeIdx := strings.Index(content, "]")
			if closeIdx == -1 {
				return nil, fmt.Errorf("missing ']' in group entry")
			}
			moduleContent := content[:closeIdx]
			content = content[closeIdx+1:]

			modules, err := parseModuleEntries(moduleContent)
			if err != nil {
				return nil, fmt.Errorf("in group '%s': %w", groupName, err)
			}
			for mod, tags := range modules {
				result[groupName][mod] = tags
			}
		}
	}
	return result, nil
}

// parseModuleEntries parses "mod1(tag1,tag2),mod2,mod3(tag3)" into module → tags
func parseModuleEntries(content string) (map[string][]string, error) {
	result := make(map[string][]string)

	for len(content) > 0 {
		if content[0] == ',' {
			content = content[1:]
			continue
		}

		modEnd := len(content)
		hasTags := false
		for i := 0; i < len(content); i++ {
			if content[i] == '(' {
				modEnd = i
				hasTags = true
				break
			}
			if content[i] == ',' {
				modEnd = i
				break
			}
		}

		modName := content[:modEnd]
		if modName == "" {
			return nil, fmt.Errorf("empty module name")
		}
		content = content[modEnd:]

		if hasTags {
			content = content[1:]
			closeIdx := strings.Index(content, ")")
			if closeIdx == -1 {
				return nil, fmt.Errorf("missing ')' for module '%s'", modName)
			}
			tagList := content[:closeIdx]
			content = content[closeIdx+1:]

			tags := parseTagList(tagList)
			result[modName] = tags
		} else {
			result[modName] = nil
		}
	}
	return result, nil
}

// parseTagList splits comma-separated tags
func parseTagList(content string) []string {
	if content == "" {
		return nil
	}
	parts := strings.Split(content, ",")
	tags := make([]string, 0, len(parts))
	for _, t := range parts {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
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

// computeReverseDeps maps local import paths to lists of files that import them.
// Keys are strictly directory paths (e.g. "pkg/sys", ".")
func computeReverseDeps(index *Index) map[string][]string {
	reverse := make(map[string][]string)

	for path, fi := range index.Files {
		// fi.Imports now contains exact relative paths or "."
		for _, impPath := range fi.Imports {
			// Verify the imported path actually exists in our index to ensure it is local
			if _, exists := index.Packages[impPath]; exists {
				reverse[impPath] = append(reverse[impPath], path)
			}
		}
	}

	for dir := range reverse {
		sort.Strings(reverse[dir])
		reverse[dir] = uniqueStrings(reverse[dir])
	}

	return reverse
}

func uniqueStrings(s []string) []string {
	if len(s) == 0 {
		return s
	}
	res := make([]string, 0, len(s))
	prev := ""
	for i, v := range s {
		if i == 0 || v != prev {
			res = append(res, v)
			prev = v
		}
	}
	return res
}