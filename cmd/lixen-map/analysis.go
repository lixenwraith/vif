package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
)

// AnalyzeFileDependencies parses a Go file to extract symbol usage from imports.
func AnalyzeFileDependencies(path string, modPath string) (*DependencyAnalysis, error) {
	fset := token.NewFileSet()
	// Parse full file (not ImportsOnly) to traverse AST
	node, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	analysis := &DependencyAnalysis{
		UsedSymbols: make(map[string][]string),
	}

	// Map local identifier (alias or name) to full import path
	imports := make(map[string]string)

	// Pre-scan imports
	for _, imp := range node.Imports {
		if imp.Path == nil {
			continue
		}
		fullPath := strings.Trim(imp.Path.Value, `"`)

		var localName string
		if imp.Name != nil {
			localName = imp.Name.Name
		} else {
			// Default package name - we have to guess or assume it matches last segment
			// For robustness without full type checking, we assume last segment.
			parts := strings.Split(fullPath, "/")
			localName = parts[len(parts)-1]
		}

		// Handle "." imports? Too complex for heuristic AST scan, skipping.
		if localName == "." || localName == "_" {
			continue
		}

		imports[localName] = fullPath
	}

	// Visit AST nodes
	ast.Inspect(node, func(n ast.Node) bool {
		// Look for selector expressions: fmt.Println, pkg.Func
		if sel, ok := n.(*ast.SelectorExpr); ok {
			// Check if X is an identifier (the package name)
			if id, ok := sel.X.(*ast.Ident); ok {
				if importPath, isImport := imports[id.Name]; isImport {
					// Found usage of importPath
					symbol := sel.Sel.Name

					// Only record symbols for internal modules if requested,
					// but requirement is to show symbols for local repo.
					if strings.HasPrefix(importPath, modPath) {
						analysis.UsedSymbols[importPath] = appendUnique(analysis.UsedSymbols[importPath], symbol)
					}
					// For external libs, we don't record symbols to save space/noise,
					// effectively treating them as "package only" in UI logic.
				}
			}
		}
		return true
	})

	// Sort symbols for stability
	for k := range analysis.UsedSymbols {
		sort.Strings(analysis.UsedSymbols[k])
	}

	return analysis, nil
}