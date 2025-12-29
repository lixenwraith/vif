package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/terminal/tui"
)

// ViewerLine represents a single line in the file viewer
// Phase 2 will add Spans []StyledSpan for AST-based highlighting
type ViewerLine struct {
	Text    string
	LineNum int  // 1-indexed display line number
	Folded  bool // True if this line starts a collapsed region
	FoldEnd int  // End line of fold region (0 if not foldable)
	Hidden  bool // True if inside a collapsed fold
}

// StyledSpan represents a styled text segment within a line
// Populated by AST parsing in Phase 2
type StyledSpan struct {
	Start int // Byte offset in line
	End   int // Byte offset end (exclusive)
	Style tui.Style
}

// FoldRegion represents a collapsible code block
type FoldRegion struct {
	StartLine int    // 0-indexed
	EndLine   int    // 0-indexed, inclusive
	Label     string // e.g., "func main()"
	Collapsed bool
}

// FileViewerState manages file viewer overlay state
type FileViewerState struct {
	Visible  bool
	FilePath string

	Lines     []ViewerLine
	FoldState map[int]bool // StartLine → collapsed

	Cursor    int // Current line index in visible lines
	Scroll    int // First visible line index
	ViewportH int // Updated during render

	SearchMode  bool
	SearchField *tui.TextFieldState
	SearchQuery string
	Matches     []int // Indices into Lines of matches
	MatchIndex  int   // Current match (-1 if none)
}

// NewFileViewerState creates initialized viewer state
func NewFileViewerState() *FileViewerState {
	return &FileViewerState{
		FoldState:   make(map[int]bool),
		SearchField: tui.NewTextFieldState(""),
		MatchIndex:  -1,
	}
}

// OpenFileViewer loads a file and displays the viewer overlay
func (app *AppState) OpenFileViewer(path string) {
	if app.Viewer == nil {
		app.Viewer = NewFileViewerState()
	}

	lines, err := loadFileLines(path)
	if err != nil {
		app.Message = "failed to open: " + err.Error()
		return
	}

	app.Viewer.FilePath = path
	app.Viewer.Lines = lines
	app.Viewer.Cursor = 0
	app.Viewer.Scroll = 0
	app.Viewer.SearchMode = false
	app.Viewer.SearchQuery = ""
	app.Viewer.Matches = nil
	app.Viewer.MatchIndex = -1
	app.Viewer.Visible = true

	// Phase 2: Call ParseFileAST here for fold regions and styled spans
}

// CloseFileViewer hides the viewer overlay
func (app *AppState) CloseFileViewer() {
	if app.Viewer != nil {
		app.Viewer.Visible = false
	}
}

// loadFileLines reads file and creates basic ViewerLine slice
func loadFileLines(path string) ([]ViewerLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []ViewerLine
	scanner := bufio.NewScanner(f)
	lineNum := 1

	for scanner.Scan() {
		lines = append(lines, ViewerLine{
			Text:    scanner.Text(),
			LineNum: lineNum,
		})
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Ensure at least one line for empty files
	if len(lines) == 0 {
		lines = append(lines, ViewerLine{Text: "", LineNum: 1})
	}

	return lines, nil
}

// visibleLines returns lines that are not hidden by folding
func (v *FileViewerState) visibleLines() []ViewerLine {
	// Phase 1: No folding, return all
	// Phase 2: Filter out Hidden lines
	result := make([]ViewerLine, 0, len(v.Lines))
	for _, line := range v.Lines {
		if !line.Hidden {
			result = append(result, line)
		}
	}
	return result
}

// handleViewerEvent processes keyboard input for the file viewer
func (app *AppState) handleViewerEvent(ev terminal.Event) {
	v := app.Viewer
	if v == nil {
		return
	}

	if v.SearchMode {
		app.handleViewerSearchEvent(ev)
		return
	}

	visible := v.visibleLines()
	total := len(visible)

	switch ev.Key {
	case terminal.KeyEscape, terminal.KeyCtrlC:
		app.CloseFileViewer()
		return

	case terminal.KeyUp:
		v.moveCursor(-1, total)
	case terminal.KeyDown:
		v.moveCursor(1, total)
	case terminal.KeyPageUp, terminal.KeyCtrlU:
		v.moveCursor(-v.ViewportH/2, total)
	case terminal.KeyPageDown, terminal.KeyCtrlD:
		v.moveCursor(v.ViewportH/2, total)
	case terminal.KeyHome:
		v.Cursor = 0
		v.adjustScroll(total)
	case terminal.KeyEnd:
		if total > 0 {
			v.Cursor = total - 1
		}
		v.adjustScroll(total)

	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			app.CloseFileViewer()
			return
		case 'j':
			v.moveCursor(1, total)
		case 'k':
			v.moveCursor(-1, total)
		case 'g':
			v.Cursor = 0
			v.adjustScroll(total)
		case 'G':
			if total > 0 {
				v.Cursor = total - 1
			}
			v.adjustScroll(total)
		case '/':
			v.SearchMode = true
			v.SearchField.Clear()
		case 'n':
			app.viewerNextMatch(1)
		case 'N':
			app.viewerNextMatch(-1)
			// Phase 2: Add fold toggle keys
			// case 'l', 'o':
			//     app.viewerToggleFold()
			// case 'h':
			//     app.viewerCollapseFold()
			// case 'z':
			//     // Wait for next key: M = collapse all, R = expand all
		}
	}
}

// handleViewerSearchEvent processes input during search mode
func (app *AppState) handleViewerSearchEvent(ev terminal.Event) {
	v := app.Viewer

	switch ev.Key {
	case terminal.KeyEscape:
		v.SearchMode = false
		return

	case terminal.KeyEnter:
		v.SearchMode = false
		v.SearchQuery = v.SearchField.Value()
		app.executeViewerSearch()
		return

	default:
		v.SearchField.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// executeViewerSearch finds all lines matching the search query
func (app *AppState) executeViewerSearch() {
	v := app.Viewer
	if v.SearchQuery == "" {
		v.Matches = nil
		v.MatchIndex = -1
		return
	}

	query := strings.ToLower(v.SearchQuery)
	visible := v.visibleLines()
	v.Matches = nil

	for i, line := range visible {
		if strings.Contains(strings.ToLower(line.Text), query) {
			v.Matches = append(v.Matches, i)
		}
	}

	if len(v.Matches) > 0 {
		// Jump to first match at or after cursor
		v.MatchIndex = 0
		for i, m := range v.Matches {
			if m >= v.Cursor {
				v.MatchIndex = i
				break
			}
		}
		v.Cursor = v.Matches[v.MatchIndex]
		v.adjustScroll(len(visible))
	} else {
		v.MatchIndex = -1
	}
}

// viewerNextMatch jumps to next/prev search match
func (app *AppState) viewerNextMatch(delta int) {
	v := app.Viewer
	if len(v.Matches) == 0 {
		return
	}

	v.MatchIndex += delta
	if v.MatchIndex >= len(v.Matches) {
		v.MatchIndex = 0
	}
	if v.MatchIndex < 0 {
		v.MatchIndex = len(v.Matches) - 1
	}

	v.Cursor = v.Matches[v.MatchIndex]
	v.adjustScroll(len(v.visibleLines()))
}

// moveCursor adjusts cursor position by delta
func (v *FileViewerState) moveCursor(delta, total int) {
	v.Cursor += delta
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	if v.Cursor >= total {
		v.Cursor = total - 1
	}
	if v.Cursor < 0 {
		v.Cursor = 0
	}
	v.adjustScroll(total)
}

// adjustScroll ensures cursor is visible in viewport
func (v *FileViewerState) adjustScroll(total int) {
	if v.ViewportH <= 0 {
		return
	}
	if v.Cursor < v.Scroll {
		v.Scroll = v.Cursor
	}
	if v.Cursor >= v.Scroll+v.ViewportH {
		v.Scroll = v.Cursor - v.ViewportH + 1
	}
	maxScroll := total - v.ViewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
	if v.Scroll < 0 {
		v.Scroll = 0
	}
}

// renderFileViewer draws the file viewer overlay
func (app *AppState) renderFileViewer(r tui.Region) {
	v := app.Viewer
	if v == nil || !v.Visible {
		return
	}

	// Build hint text
	hint := "q:quit  /:search"
	if len(v.Matches) > 0 {
		hint = "n/N:match  " + hint
	}

	// Use Modal for fullscreen overlay
	content := r.Modal(tui.ModalOpts{
		Title:    filepath.Base(v.FilePath),
		Hint:     hint,
		Border:   tui.LineDouble,
		BorderFg: app.Theme.Border,
		TitleFg:  app.Theme.HeaderFg,
		HintFg:   app.Theme.StatusFg,
		Bg:       app.Theme.Bg,
	})

	// Reserve bottom row for search bar when active, or status
	var mainContent, statusBar tui.Region
	mainContent, statusBar = tui.SplitVFixed(content, content.H-1)

	// Calculate gutter width based on line count
	gutterW := 3
	lineCount := len(v.Lines)
	if lineCount >= 1000 {
		gutterW = 5
	} else if lineCount >= 100 {
		gutterW = 4
	}

	// Split into gutter and text area
	gutter, textArea := tui.SplitHFixed(mainContent, gutterW+1) // +1 for separator

	v.ViewportH = textArea.H
	visible := v.visibleLines()
	v.adjustScroll(len(visible))

	// Build match set for highlighting
	matchSet := make(map[int]bool)
	for _, m := range v.Matches {
		matchSet[m] = true
	}

	// Render visible lines
	for y := 0; y < textArea.H; y++ {
		idx := v.Scroll + y
		if idx >= len(visible) {
			break
		}

		line := visible[idx]
		isCursor := idx == v.Cursor
		isMatch := matchSet[idx]

		// Determine row background
		bg := app.Theme.Bg
		if isCursor {
			bg = app.Theme.CursorBg
		}

		// Gutter: line number
		gutterBg := app.Theme.Bg
		if isCursor {
			gutterBg = app.Theme.FocusBg
		}

		// Clear gutter row
		for x := 0; x < gutter.W; x++ {
			gutter.Cell(x, y, ' ', app.Theme.StatusFg, gutterBg, terminal.AttrNone)
		}

		// Right-align line number
		numStr := formatLineNumber(line.LineNum, gutterW)
		for i, ch := range numStr {
			gutter.Cell(i, y, ch, app.Theme.StatusFg, gutterBg, terminal.AttrDim)
		}

		// Separator
		gutter.Cell(gutterW, y, '│', app.Theme.Border, gutterBg, terminal.AttrDim)

		// Clear text row
		for x := 0; x < textArea.W; x++ {
			textArea.Cell(x, y, ' ', app.Theme.Fg, bg, terminal.AttrNone)
		}

		// Render line text
		// Phase 1: Plain text
		// Phase 2: Render StyledSpans from AST
		text := line.Text
		fg := app.Theme.Fg
		attr := terminal.AttrNone

		// Highlight search matches in line
		if isMatch && v.SearchQuery != "" {
			fg = app.Theme.TagFg // Use accent color for match lines
		}

		// Fold indicator (Phase 2)
		// if line.Folded {
		//     textArea.Cell(0, y, '▶', app.Theme.HintFg, bg, terminal.AttrNone)
		// }

		// Render text with tab expansion
		x := 0
		for _, ch := range text {
			if x >= textArea.W {
				break
			}
			if ch == '\t' {
				spaces := 4 - (x % 4)
				for s := 0; s < spaces && x < textArea.W; s++ {
					textArea.Cell(x, y, ' ', fg, bg, attr)
					x++
				}
			} else {
				textArea.Cell(x, y, ch, fg, bg, attr)
				x++
			}
		}
	}

	// Status bar / search bar
	statusBar.Fill(app.Theme.Bg)

	if v.SearchMode {
		// Render search input
		statusBar.TextField(v.SearchField, tui.TextFieldOpts{
			Prefix:  "/",
			Focused: true,
			Style: tui.TextFieldStyle{
				TextFg:   app.Theme.HeaderFg,
				TextBg:   app.Theme.InputBg,
				CursorFg: app.Theme.Bg,
				CursorBg: app.Theme.HeaderFg,
				PrefixFg: app.Theme.StatusFg,
			},
		})
	} else {
		// Status line
		var status string
		if len(v.Matches) > 0 {
			status = formatSearchStatus(v.MatchIndex+1, len(v.Matches), v.SearchQuery)
		} else if v.SearchQuery != "" {
			status = "no matches: " + v.SearchQuery
		}

		// Left: status or file path
		leftText := status
		if leftText == "" {
			leftText = v.FilePath
		}
		statusBar.Text(1, 0, leftText, app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)

		// Right: position
		posText := formatPosition(v.Cursor+1, len(visible))
		statusBar.TextRight(0, posText, app.Theme.StatusFg, app.Theme.Bg, terminal.AttrNone)
	}
}

// formatLineNumber formats line number right-aligned
func formatLineNumber(num, width int) string {
	s := ""
	for num > 0 {
		s = string(rune('0'+num%10)) + s
		num /= 10
	}
	if s == "" {
		s = "0"
	}
	for len(s) < width {
		s = " " + s
	}
	return s
}

// formatSearchStatus formats search match indicator
func formatSearchStatus(current, total int, query string) string {
	return "[" + formatLineNumber(current, 1) + "/" + formatLineNumber(total, 1) + "] " + query
}

// formatPosition formats cursor position indicator
func formatPosition(line, total int) string {
	return formatLineNumber(line, 1) + "/" + formatLineNumber(total, 1) + " "
}