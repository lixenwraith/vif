package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// EnterEditMode initiates inline tag editing for the file at cursor
func (app *AppState) EnterEditMode() {
	if app.FocusPane != PaneTree {
		app.Message = "edit only from file tree"
		return
	}

	if len(app.TreeFlat) == 0 {
		return
	}

	node := app.TreeFlat[app.TreeCursor]
	if node.IsDir {
		app.Message = "select a file to edit tags"
		return
	}

	app.EditTarget = node.Path
	app.EditMode = true

	content, err := readLixenLine(node.Path)
	if err != nil {
		app.Message = fmt.Sprintf("read error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		return
	}

	// Canonicalize for consistent editing
	categories, err := parseLixenContent(content)
	if err != nil {
		app.InputBuffer = content // Fallback to raw on parse error
		app.EditCursor = len(content)
		return
	}

	app.InputBuffer = canonicalizeLixenContent(categories)
	app.EditCursor = len(app.InputBuffer)
}

// HandleEditEvent processes keyboard input during tag editing
func (app *AppState) HandleEditEvent(ev terminal.Event) {
	buf := []rune(app.InputBuffer)
	cursor := app.EditCursor

	// Clamp cursor
	if cursor > len(buf) {
		cursor = len(buf)
	}
	if cursor < 0 {
		cursor = 0
	}

	switch ev.Key {
	case terminal.KeyEscape:
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		app.EditCursor = 0
		app.Message = "edit cancelled"

	case terminal.KeyEnter:
		app.commitTagEdit()

	case terminal.KeyBackspace:
		if cursor > 0 {
			buf = append(buf[:cursor-1], buf[cursor:]...)
			cursor--
		}

	case terminal.KeyDelete:
		if cursor < len(buf) {
			buf = append(buf[:cursor], buf[cursor+1:]...)
		}

	case terminal.KeyLeft:
		if cursor > 0 {
			cursor--
		}

	case terminal.KeyRight:
		if cursor < len(buf) {
			cursor++
		}

	case terminal.KeyHome, terminal.KeyCtrlA:
		cursor = 0

	case terminal.KeyEnd, terminal.KeyCtrlE:
		cursor = len(buf)

	case terminal.KeyCtrlK:
		// Kill to end of line
		buf = buf[:cursor]

	case terminal.KeyCtrlU:
		// Kill to start of line
		buf = buf[cursor:]
		cursor = 0

	case terminal.KeyCtrlW:
		// Kill previous word
		if cursor > 0 {
			// Skip trailing spaces
			end := cursor
			for end > 0 && buf[end-1] == ' ' {
				end--
			}
			// Find word start
			start := end
			for start > 0 && buf[start-1] != ' ' {
				start--
			}
			buf = append(buf[:start], buf[end:]...)
			cursor = start
		}

	case terminal.KeyRune:
		// Insert at cursor
		newBuf := make([]rune, len(buf)+1)
		copy(newBuf[:cursor], buf[:cursor])
		newBuf[cursor] = ev.Rune
		copy(newBuf[cursor+1:], buf[cursor:])
		buf = newBuf
		cursor++
	}

	app.InputBuffer = string(buf)
	app.EditCursor = cursor
}

// commitTagEdit writes modified tags to file and triggers reindex
func (app *AppState) commitTagEdit() {
	path := app.EditTarget
	content := strings.TrimSpace(app.InputBuffer)

	if err := writeLixenLine(path, content); err != nil {
		app.Message = fmt.Sprintf("write error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		app.EditCursor = 0
		return
	}

	app.EditMode = false
	app.EditTarget = ""
	app.InputBuffer = ""
	app.EditCursor = 0

	app.ReindexAll()
	app.Message = fmt.Sprintf("updated: %s", path)
}

// readLixenLine extracts and merges all @lixen: content from file header
func readLixenLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var contents []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(trimmed, "package ") {
			break
		}

		if strings.HasPrefix(trimmed, "// @lixen:") {
			content := strings.TrimPrefix(trimmed, "// @lixen:")
			content = strings.TrimSpace(content)
			if content != "" {
				contents = append(contents, content)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	if len(contents) == 0 {
		return "", nil
	}

	// Merge multiple lines by parsing and re-canonicalizing
	merged := make(map[string]map[string]map[string][]string)

	for _, c := range contents {
		categories, err := parseLixenContent(c)
		if err != nil {
			continue // Skip malformed lines
		}
		for cat, groups := range categories {
			if merged[cat] == nil {
				merged[cat] = make(map[string]map[string][]string)
			}
			for g, mods := range groups {
				if merged[cat][g] == nil {
					merged[cat][g] = make(map[string][]string)
				}
				for m, tags := range mods {
					merged[cat][g][m] = appendUnique(merged[cat][g][m], tags...)
				}
			}
		}
	}

	return canonicalizeLixenContent(merged), nil
}

// appendUnique appends values not already present
func appendUnique(slice []string, values ...string) []string {
	seen := make(map[string]bool, len(slice))
	for _, s := range slice {
		seen[s] = true
	}
	for _, v := range values {
		if !seen[v] {
			slice = append(slice, v)
			seen[v] = true
		}
	}
	return slice
}

// parseLixenContent parses lixen content into category map
// Returns: category → group → module → tags
func parseLixenContent(content string) (map[string]map[string]map[string][]string, error) {
	if content == "" {
		return make(map[string]map[string]map[string][]string), nil
	}

	fi := &FileInfo{
		Tags: make(map[string]map[string]map[string][]string),
	}

	content = stripWhitespace(content)
	if err := parseBlocks(content, fi); err != nil {
		return nil, err
	}

	return fi.Tags, nil
}

// canonicalizeLixenContent creates canonical lixen content from category map
// Output format: #category1{...},#category2{...}
func canonicalizeLixenContent(categories map[string]map[string]map[string][]string) string {
	if len(categories) == 0 {
		return ""
	}

	// Sort category names
	catNames := make([]string, 0, len(categories))
	for cat := range categories {
		catNames = append(catNames, cat)
	}
	sort.Strings(catNames)

	var parts []string
	for _, cat := range catNames {
		groups := categories[cat]
		if block := formatCategoryBlock(cat, groups); block != "" {
			parts = append(parts, block)
		}
	}

	return strings.Join(parts, ",")
}

// formatCategoryBlock formats a single category block
func formatCategoryBlock(category string, groups map[string]map[string][]string) string {
	if len(groups) == 0 {
		return ""
	}

	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	var groupParts []string
	for _, g := range groupNames {
		mods := groups[g]

		// Check if only DirectTagsModule exists (2-level format)
		if len(mods) == 1 {
			if tags, ok := mods[DirectTagsModule]; ok && len(tags) > 0 {
				sortedTags := make([]string, len(tags))
				copy(sortedTags, tags)
				sort.Strings(sortedTags)
				groupParts = append(groupParts, fmt.Sprintf("%s(%s)", g, strings.Join(sortedTags, ",")))
				continue
			}
		}

		// 3-level format: group[mod1(tag1),mod2]
		modNames := make([]string, 0, len(mods))
		for m := range mods {
			if m != DirectTagsModule {
				modNames = append(modNames, m)
			}
		}
		sort.Strings(modNames)

		// Include DirectTagsModule tags as direct if present alongside modules
		var modParts []string
		if tags, ok := mods[DirectTagsModule]; ok && len(tags) > 0 {
			sortedTags := make([]string, len(tags))
			copy(sortedTags, tags)
			sort.Strings(sortedTags)
			// Direct tags go first without module wrapper
			for _, t := range sortedTags {
				modParts = append(modParts, t)
			}
		}

		for _, m := range modNames {
			tags := mods[m]
			if len(tags) == 0 {
				modParts = append(modParts, m)
			} else {
				sortedTags := make([]string, len(tags))
				copy(sortedTags, tags)
				sort.Strings(sortedTags)
				modParts = append(modParts, fmt.Sprintf("%s(%s)", m, strings.Join(sortedTags, ",")))
			}
		}

		if len(modParts) > 0 {
			groupParts = append(groupParts, fmt.Sprintf("%s[%s]", g, strings.Join(modParts, ",")))
		}
	}

	if len(groupParts) == 0 {
		return ""
	}

	return fmt.Sprintf("#%s{%s}", category, strings.Join(groupParts, ","))
}

// writeLixenLine atomically writes lixen line to file, removing all existing @lixen: lines
func writeLixenLine(path, content string) error {
	categories, err := parseLixenContent(content)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	canonical := canonicalizeLixenContent(categories)

	fileContent, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(fileContent), "\n")

	// Find all @lixen: lines and package line
	var lixenIndices []int
	packageIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// @lixen:") {
			lixenIndices = append(lixenIndices, i)
		}
		if strings.HasPrefix(trimmed, "package ") {
			packageIdx = i
			break
		}
	}

	var newLines []string

	if len(lixenIndices) > 0 {
		// Remove all existing @lixen: lines, insert single canonical at first position
		newLines = make([]string, 0, len(lines)-len(lixenIndices)+1)
		removed := make(map[int]bool)
		for _, idx := range lixenIndices {
			removed[idx] = true
		}

		insertDone := false
		for i, line := range lines {
			if removed[i] {
				// Insert canonical line at position of first removed line
				if !insertDone && canonical != "" {
					newLines = append(newLines, "// @lixen: "+canonical)
					insertDone = true
				}
				continue
			}
			newLines = append(newLines, line)
		}
	} else if canonical != "" {
		// No existing line - insert before package, after build tags
		insertIdx := 0
		if packageIdx > 0 {
			for i := 0; i < packageIdx; i++ {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == "" || strings.HasPrefix(trimmed, "//go:build") || strings.HasPrefix(trimmed, "// +build") {
					insertIdx = i + 1
				} else {
					break
				}
			}
		}

		newLines = make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertIdx]...)
		newLines = append(newLines, "// @lixen: "+canonical)
		newLines = append(newLines, lines[insertIdx:]...)
	} else {
		return nil // No content and no existing lines
	}

	result := strings.Join(newLines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	// Atomic write
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".lixen-edit-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(result); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if info, err := os.Stat(path); err == nil {
		os.Chmod(tmpPath, info.Mode())
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}