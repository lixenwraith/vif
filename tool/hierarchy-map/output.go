package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteOutputFile writes file paths to catalog output file
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

// LoadSelectionFile reads catalog file and returns matched paths
// Supports glob patterns; lines without globs are treated as literal paths
func LoadSelectionFile(path string, index *Index) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	matched := make(map[string]bool)
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip ./ prefix if present
		line = strings.TrimPrefix(line, "./")

		if strings.Contains(line, "*") {
			// Glob pattern - expand recursively
			paths := expandGlob(line, index)
			for _, p := range paths {
				matched[p] = true
			}
		} else {
			// Literal path - validate exists in index
			if _, ok := index.Files[line]; ok {
				matched[line] = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]string, 0, len(matched))
	for p := range matched {
		result = append(result, p)
	}
	sort.Strings(result)
	return result, nil
}

// expandGlob matches pattern against indexed files
// Supports simple globs: * matches any sequence within path segment
// For patterns like "cmd/*" or "cmd/*.go", matches all files under cmd/
func expandGlob(pattern string, index *Index) []string {
	var matches []string

	// Check if pattern is directory prefix (ends with /* or contains /*)
	if strings.HasSuffix(pattern, "/*") || strings.HasSuffix(pattern, "/**") {
		// Directory recursive match
		prefix := strings.TrimSuffix(strings.TrimSuffix(pattern, "**"), "*")
		prefix = strings.TrimSuffix(prefix, "/")
		for path := range index.Files {
			if strings.HasPrefix(path, prefix+"/") || path == prefix {
				matches = append(matches, path)
			}
		}
		return matches
	}

	// Use filepath.Match for standard glob patterns
	for path := range index.Files {
		ok, err := filepath.Match(pattern, path)
		if err == nil && ok {
			matches = append(matches, path)
			continue
		}

		// Also try matching just the filename for patterns like "*_test.go"
		if !strings.Contains(pattern, "/") {
			ok, err = filepath.Match(pattern, filepath.Base(path))
			if err == nil && ok {
				matches = append(matches, path)
			}
		}
	}

	return matches
}