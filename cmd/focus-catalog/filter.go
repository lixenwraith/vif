package main

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// SearchKeyword shells to rg for content search
func SearchKeyword(root, pattern string, caseSensitive bool) ([]string, error) {
	args := []string{"--files-with-matches", "-g", "*.go"}
	if !caseSensitive {
		args = append(args, "-i")
	}
	args = append(args, "--", pattern, root)

	cmd := exec.Command("rg", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // No matches
		}
		return nil, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}

	lines := strings.Split(raw, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimPrefix(l, "./")
		l = filepath.ToSlash(l)
		result = append(result, l)
	}

	return result, nil
}

// UpdatePackageList filters packages based on current group/keyword
func (app *AppState) UpdatePackageList() {
	app.PackageList = make([]string, 0, len(app.AllPackages))

	for _, name := range app.AllPackages {
		pkg := app.Index.Packages[name]

		// Group filter
		if app.ActiveGroup != "" {
			if _, ok := pkg.AllTags[app.ActiveGroup]; !ok && !pkg.HasAll {
				continue
			}
		}

		// Keyword filter
		if app.KeywordFilter != "" && len(app.KeywordMatches) > 0 {
			hasMatch := false
			for _, f := range pkg.Files {
				if app.KeywordMatches[f.Path] {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				continue
			}
		}

		app.PackageList = append(app.PackageList, name)
	}

	// Adjust cursor if out of bounds
	if app.CursorPos >= len(app.PackageList) {
		app.CursorPos = len(app.PackageList) - 1
	}
	if app.CursorPos < 0 {
		app.CursorPos = 0
	}
}
