package main

import (
	"path/filepath"
	"sort"
	"strings"
)

// BuildTree constructs hierarchical tree from flat index.
func BuildTree(index *Index) *TreeNode {
	root := &TreeNode{
		Name:     ".",
		Path:     ".",
		IsDir:    true,
		Expanded: true,
		Children: make([]*TreeNode, 0),
	}

	dirNodes := make(map[string]*TreeNode)
	dirNodes["."] = root

	dirs := make([]string, 0, len(index.Packages))
	for dir := range index.Packages {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		ensureDirNode(root, dir, dirNodes, index)
	}

	for dir, pkg := range index.Packages {
		dirNode := dirNodes[dir]
		if dirNode == nil {
			dirNode = root
		}

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

	sortChildren(root)
	return root
}

// computeReverseDeps builds map of packages to their importers.
func computeReverseDeps(index *Index) map[string][]string {
	reverse := make(map[string][]string)

	for dir, pkg := range index.Packages {
		for _, dep := range pkg.LocalDeps {
			for depDir, depPkg := range index.Packages {
				if depPkg.Name == dep {
					reverse[depDir] = append(reverse[depDir], dir)
					break
				}
			}
		}
	}

	for dir := range reverse {
		sort.Strings(reverse[dir])
	}

	return reverse
}

// ensureDirNode creates directory node and ancestors as needed.
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

		var pkgInfo *PackageInfo
		if pkg, ok := index.Packages[currentPath]; ok {
			pkgInfo = pkg
		}

		node := &TreeNode{
			Name:        part,
			Path:        currentPath,
			IsDir:       true,
			Expanded:    i == 0,
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

// sortChildren recursively sorts tree nodes (dirs first, then alphabetical).
func sortChildren(node *TreeNode) {
	if len(node.Children) == 0 {
		return
	}

	sort.Slice(node.Children, func(i, j int) bool {
		if node.Children[i].IsDir != node.Children[j].IsDir {
			return node.Children[i].IsDir
		}
		return node.Children[i].Name < node.Children[j].Name
	})

	for _, child := range node.Children {
		sortChildren(child)
	}
}

// FlattenTree creates ordered list of visible nodes for rendering.
func FlattenTree(root *TreeNode) []*TreeNode {
	var result []*TreeNode
	flattenNode(root, &result, true)
	return result
}

// flattenNode recursively appends visible nodes to result slice.
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

// collectFiles recursively collects file paths under a tree node.
func collectFiles(node *TreeNode, files *[]string) {
	if !node.IsDir {
		*files = append(*files, node.Path)
		return
	}
	for _, child := range node.Children {
		collectFiles(child, files)
	}
}

// collapseAllRecursive recursively collapses directory nodes.
func collapseAllRecursive(node *TreeNode) {
	if node.IsDir && node.Path != "." {
		node.Expanded = false
	}
	for _, child := range node.Children {
		collapseAllRecursive(child)
	}
}

// expandAllRecursive recursively expands directory nodes.
func expandAllRecursive(node *TreeNode) {
	if node.IsDir {
		node.Expanded = true
	}
	for _, child := range node.Children {
		expandAllRecursive(child)
	}
}