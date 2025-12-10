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
		AllTags:    make(map[string][]string),
	}

	groupSet := make(map[string]bool)
	tagsByGroup := make(map[string]map[string]bool)

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

		// Merge tags into package and index
		for group, tags := range fi.Tags {
			groupSet[group] = true

			// Package tags
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

			// Index-level tags
			if tagsByGroup[group] == nil {
				tagsByGroup[group] = make(map[string]bool)
			}
			for _, t := range tags {
				tagsByGroup[group][t] = true
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

	// Build sorted groups list
	for g := range groupSet {
		if g != "all" {
			index.Groups = append(index.Groups, g)
		}
	}
	sort.Strings(index.Groups)

	// Build sorted tags per group
	for group, tagSet := range tagsByGroup {
		tags := make([]string, 0, len(tagSet))
		for t := range tagSet {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		index.AllTags[group] = tags
	}

	return index, nil
}

// BuildTree constructs a tree from the index
func BuildTree(index *Index) *TreeNode {
	root := &TreeNode{
		Name:     ".",
		Path:     ".",
		IsDir:    true,
		Expanded: true,
		Children: make([]*TreeNode, 0),
	}

	// Map dir path → node for building hierarchy
	dirNodes := make(map[string]*TreeNode)
	dirNodes["."] = root

	// Collect all directories from packages
	dirs := make([]string, 0, len(index.Packages))
	for _, pkg := range index.Packages {
		dirs = append(dirs, pkg.Dir)
	}
	sort.Strings(dirs)

	// Create directory nodes
	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		ensureDirNode(root, dir, dirNodes, index)
	}

	// Add files to their package directories
	for _, pkg := range index.Packages {
		dirNode := dirNodes[pkg.Dir]
		if dirNode == nil {
			dirNode = root
		}

		// Sort files by name
		files := make([]*FileInfo, len(pkg.Files))
		copy(files, pkg.Files)
		sort.Slice(files, func(i, j int) bool {
			return filepath.Base(files[i].Path) < filepath.Base(files[j].Path)
		})

		for _, fi := range files {
			fileNode := &TreeNode{
				Name:     filepath.Base(fi.Path),
				Path:     fi.Path,
				IsDir:    false,
				FileInfo: fi,
				Parent:   dirNode,
				Depth:    dirNode.Depth + 1,
			}
			dirNode.Children = append(dirNode.Children, fileNode)
		}
	}

	// Sort children: directories first, then files, alphabetically within each
	sortChildren(root)

	return root
}

// ensureDirNode creates directory node and all parent nodes as needed
func ensureDirNode(root *TreeNode, dir string, dirNodes map[string]*TreeNode, index *Index) *TreeNode {
	if node, ok := dirNodes[dir]; ok {
		return node
	}

	parts := strings.Split(dir, "/")
	current := root
	currentPath := ""

	for i, part := range parts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		if node, ok := dirNodes[currentPath]; ok {
			current = node
			continue
		}

		// Find package info if this is a package directory
		var pkgInfo *PackageInfo
		for _, pkg := range index.Packages {
			if pkg.Dir == currentPath {
				pkgInfo = pkg
				break
			}
		}

		node := &TreeNode{
			Name:        part,
			Path:        currentPath,
			IsDir:       true,
			Expanded:    i == 0, // Expand top-level by default
			Children:    make([]*TreeNode, 0),
			Parent:      current,
			PackageInfo: pkgInfo,
			Depth:       current.Depth + 1,
		}
		current.Children = append(current.Children, node)
		dirNodes[currentPath] = node
		current = node
	}

	return current
}

// sortChildren recursively sorts tree nodes
func sortChildren(node *TreeNode) {
	if len(node.Children) == 0 {
		return
	}

	sort.Slice(node.Children, func(i, j int) bool {
		// Directories first
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		return node.Children[i].Name < node.Children[j].Name
	})

	for _, child := range node.Children {
		sortChildren(child)
	}
}

// FlattenTree creates a flat list of visible nodes for rendering
func FlattenTree(root *TreeNode) []*TreeNode {
	var result []*TreeNode
	flattenNode(root, &result, true)
	return result
}

func flattenNode(node *TreeNode, result *[]*TreeNode, skipRoot bool) {
	if !skipRoot {
		*result = append(*result, node)
	}

	if node.IsDir && node.Expanded {
		for _, child := range node.Children {
			flattenNode(child, result, false)
		}
	}
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

	for len(line) > 0 {
		idx := strings.Index(line, "#")
		if idx == -1 {
			break
		}
		line = line[idx+1:]

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

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			result[groupName] = []string{}
			continue
		}

		line = line[1:]
		endBrace := strings.Index(line, "}")
		if endBrace == -1 {
			break
		}

		tagsStr := line[:endBrace]
		line = line[endBrace+1:]

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