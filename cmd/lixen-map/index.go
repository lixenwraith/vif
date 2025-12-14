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
		ModulePath:       modPath,
		Packages:         make(map[string]*PackageInfo),
		Files:            make(map[string]*FileInfo),
		FocusModules:     make(map[string][]string),
		FocusTags:        make(map[string]map[string][]string),
		FocusByGroup:     make(map[string][]string),
		FocusByModule:    make(map[string][]string),
		FocusByTag:       make(map[string][]string),
		InteractModules:  make(map[string][]string),
		InteractTags:     make(map[string]map[string][]string),
		InteractByGroup:  make(map[string][]string),
		InteractByModule: make(map[string][]string),
		InteractByTag:    make(map[string][]string),
	}

	focusGroupSet := make(map[string]bool)
	focusModulesByGroup := make(map[string]map[string]bool)
	focusTagsByGroupModule := make(map[string]map[string]map[string]bool)

	interactGroupSet := make(map[string]bool)
	interactModulesByGroup := make(map[string]map[string]bool)
	interactTagsByGroupModule := make(map[string]map[string]map[string]bool)

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
		for group, modules := range fi.Focus {
			focusGroupSet[group] = true
			if focusModulesByGroup[group] == nil {
				focusModulesByGroup[group] = make(map[string]bool)
			}
			if focusTagsByGroupModule[group] == nil {
				focusTagsByGroupModule[group] = make(map[string]map[string]bool)
			}

			index.FocusByGroup[group] = append(index.FocusByGroup[group], relPath)

			for module, tags := range modules {
				if module != DirectTagsModule {
					focusModulesByGroup[group][module] = true
					moduleKey := group + "." + module
					index.FocusByModule[moduleKey] = append(index.FocusByModule[moduleKey], relPath)
				}

				if focusTagsByGroupModule[group][module] == nil {
					focusTagsByGroupModule[group][module] = make(map[string]bool)
				}

				for _, tag := range tags {
					focusTagsByGroupModule[group][module][tag] = true
					var tagKey string
					if module == DirectTagsModule {
						tagKey = group + ".." + tag // double dot for 2-level
					} else {
						tagKey = group + "." + module + "." + tag
					}
					index.FocusByTag[tagKey] = append(index.FocusByTag[tagKey], relPath)
				}
			}
		}

		// Index interact tags
		for group, modules := range fi.Interact {
			interactGroupSet[group] = true
			if interactModulesByGroup[group] == nil {
				interactModulesByGroup[group] = make(map[string]bool)
			}
			if interactTagsByGroupModule[group] == nil {
				interactTagsByGroupModule[group] = make(map[string]map[string]bool)
			}

			index.InteractByGroup[group] = append(index.InteractByGroup[group], relPath)

			for module, tags := range modules {
				if module != DirectTagsModule {
					interactModulesByGroup[group][module] = true
					moduleKey := group + "." + module
					index.InteractByModule[moduleKey] = append(index.InteractByModule[moduleKey], relPath)
				}

				if interactTagsByGroupModule[group][module] == nil {
					interactTagsByGroupModule[group][module] = make(map[string]bool)
				}

				for _, tag := range tags {
					interactTagsByGroupModule[group][module][tag] = true
					var tagKey string
					if module == DirectTagsModule {
						tagKey = group + ".." + tag
					} else {
						tagKey = group + "." + module + "." + tag
					}
					index.InteractByTag[tagKey] = append(index.InteractByTag[tagKey], relPath)
				}
			}
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

	// Build sorted focus modules per group
	for group, modSet := range focusModulesByGroup {
		mods := make([]string, 0, len(modSet))
		for m := range modSet {
			mods = append(mods, m)
		}
		sort.Strings(mods)
		index.FocusModules[group] = mods
	}

	// Build sorted focus tags per group/module
	for group, modMap := range focusTagsByGroupModule {
		if index.FocusTags[group] == nil {
			index.FocusTags[group] = make(map[string][]string)
		}
		for module, tagSet := range modMap {
			tags := make([]string, 0, len(tagSet))
			for t := range tagSet {
				tags = append(tags, t)
			}
			sort.Strings(tags)
			index.FocusTags[group][module] = tags
		}
	}

	// Build sorted interact groups
	for g := range interactGroupSet {
		index.InteractGroups = append(index.InteractGroups, g)
	}
	sort.Strings(index.InteractGroups)

	// Build sorted interact modules per group
	for group, modSet := range interactModulesByGroup {
		mods := make([]string, 0, len(modSet))
		for m := range modSet {
			mods = append(mods, m)
		}
		sort.Strings(mods)
		index.InteractModules[group] = mods
	}

	// Build sorted interact tags per group/module
	for group, modMap := range interactTagsByGroupModule {
		if index.InteractTags[group] == nil {
			index.InteractTags[group] = make(map[string][]string)
		}
		for module, tagSet := range modMap {
			tags := make([]string, 0, len(tagSet))
			for t := range tagSet {
				tags = append(tags, t)
			}
			sort.Strings(tags)
			index.InteractTags[group][module] = tags
		}
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
		Focus:    make(map[string]map[string][]string),
		Interact: make(map[string]map[string][]string),
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
				continue
			}
		}
	}

	if fi.Package == "" {
		return nil, nil
	}

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

		blockType := content[:braceIdx]
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

		switch blockType {
		case "focus":
			for g, modules := range groups {
				if g == "all" {
					if mods, ok := modules[DirectTagsModule]; ok && len(mods) > 0 && mods[0] == "*" {
						fi.IsAll = true
						continue
					}
				}
				if fi.Focus[g] == nil {
					fi.Focus[g] = make(map[string][]string)
				}
				for mod, tags := range modules {
					fi.Focus[g][mod] = append(fi.Focus[g][mod], tags...)
				}
			}
		case "interact":
			for g, modules := range groups {
				if fi.Interact[g] == nil {
					fi.Interact[g] = make(map[string][]string)
				}
				for mod, tags := range modules {
					fi.Interact[g][mod] = append(fi.Interact[g][mod], tags...)
				}
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

		// Find group name (up to '[' or '(')
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
			// 2-level format: group(tag1,tag2)
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
			// 3-level format: group[mod1(tag1),mod2]
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

		// Find module name (up to '(' or ',' or end)
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
			// Module with tags: mod(tag1,tag2)
			content = content[1:] // skip '('
			closeIdx := strings.Index(content, ")")
			if closeIdx == -1 {
				return nil, fmt.Errorf("missing ')' for module '%s'", modName)
			}
			tagList := content[:closeIdx]
			content = content[closeIdx+1:]

			tags := parseTagList(tagList)
			result[modName] = tags
		} else {
			// Module without tags
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