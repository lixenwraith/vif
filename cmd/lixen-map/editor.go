package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// EnterEditMode initiates inline tag editing for the file at cursor
func (app *AppState) EnterEditMode() {
	if app.FocusPane != PaneLeft {
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

	// Load current focus line content
	content, err := readFocusLine(node.Path)
	if err != nil {
		app.Message = fmt.Sprintf("read error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		return
	}

	app.InputBuffer = content
}

// HandleEditEvent processes keyboard input during tag editing
func (app *AppState) HandleEditEvent(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		app.Message = "edit cancelled"
		return

	case terminal.KeyEnter:
		app.commitTagEdit()
		return

	case terminal.KeyBackspace:
		if len(app.InputBuffer) > 0 {
			app.InputBuffer = app.InputBuffer[:len(app.InputBuffer)-1]
		}
		return

	case terminal.KeyRune:
		app.InputBuffer += string(ev.Rune)
		return
	}
}

// commitTagEdit writes modified tags to file and triggers reindex
func (app *AppState) commitTagEdit() {
	path := app.EditTarget
	newTags := strings.TrimSpace(app.InputBuffer)

	err := writeFocusLine(path, newTags)
	if err != nil {
		app.Message = fmt.Sprintf("write error: %v", err)
		app.EditMode = false
		app.EditTarget = ""
		app.InputBuffer = ""
		return
	}

	// Exit edit mode
	app.EditMode = false
	app.EditTarget = ""
	app.InputBuffer = ""

	// Reindex entire tree
	app.ReindexAll()
	app.Message = fmt.Sprintf("updated tags: %s", path)
}

// readFocusLine extracts @lixen tag content from file header
func readFocusLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "package ") {
			break
		}

		// Check for lixen line (new format)
		if strings.HasPrefix(trimmed, "// @lixen:") {
			content := strings.TrimPrefix(trimmed, "// @lixen:")
			return strings.TrimSpace(content), nil
		}
	}

	return "", scanner.Err()
}

// writeFocusLine atomically writes @focus line to file, preserving structure
func writeFocusLine(path, tags string) error {
	// Read entire file
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	focusLine := fmt.Sprintf("// @focus: %s", tags)

	// Find existing focus line or package line
	focusIdx := -1
	packageIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "// @focus:") && focusIdx == -1 {
			focusIdx = i
		}

		if strings.HasPrefix(trimmed, "package ") {
			packageIdx = i
			break
		}
	}

	var newLines []string

	if focusIdx >= 0 {
		// Replace existing focus line
		newLines = make([]string, len(lines))
		copy(newLines, lines)
		newLines[focusIdx] = focusLine
	} else {
		// Insert at beginning (before package, after any build tags/comments)
		insertIdx := 0
		if packageIdx > 0 {
			// Find good insertion point - after initial comments/build tags
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
		newLines = append(newLines, focusLine)
		newLines = append(newLines, lines[insertIdx:]...)
	}

	// Ensure file ends with newline
	result := strings.Join(newLines, "\n")
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	// Atomic write: temp file + rename
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".focus-edit-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	_, err = tmpFile.WriteString(result)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}

	err = tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Preserve original file permissions
	info, err := os.Stat(path)
	if err == nil {
		os.Chmod(tmpPath, info.Mode())
	}

	// Atomic rename
	err = os.Rename(tmpPath, path)
	if err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}