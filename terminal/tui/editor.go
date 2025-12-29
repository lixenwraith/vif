// FILE: terminal/tui/editor.go
package tui

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// EditorState holds multi-line text editor state
type EditorState struct {
	Lines      []string
	CursorLine int
	CursorCol  int
	ScrollX    int
	ScrollY    int
	ViewportW  int // Updated during render
	ViewportH  int // Updated during render
}

// NewEditorState creates initialized editor state
func NewEditorState(initial string) *EditorState {
	lines := splitLines(initial)
	if len(lines) == 0 {
		lines = []string{""}
	}
	return &EditorState{
		Lines: lines,
	}
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

// Value returns all lines joined with newlines
func (e *EditorState) Value() string {
	if len(e.Lines) == 0 {
		return ""
	}
	result := e.Lines[0]
	for i := 1; i < len(e.Lines); i++ {
		result += "\n" + e.Lines[i]
	}
	return result
}

// SetValue replaces all content and resets cursor
func (e *EditorState) SetValue(s string) {
	e.Lines = splitLines(s)
	if len(e.Lines) == 0 {
		e.Lines = []string{""}
	}
	e.CursorLine = 0
	e.CursorCol = 0
	e.ScrollX = 0
	e.ScrollY = 0
}

// Clear empties the editor
func (e *EditorState) Clear() {
	e.Lines = []string{""}
	e.CursorLine = 0
	e.CursorCol = 0
	e.ScrollX = 0
	e.ScrollY = 0
}

// LineCount returns number of lines
func (e *EditorState) LineCount() int {
	return len(e.Lines)
}

// CurrentLine returns current line text
func (e *EditorState) CurrentLine() string {
	if e.CursorLine < 0 || e.CursorLine >= len(e.Lines) {
		return ""
	}
	return e.Lines[e.CursorLine]
}

// clampCursor ensures cursor is within valid bounds
func (e *EditorState) clampCursor() {
	if len(e.Lines) == 0 {
		e.Lines = []string{""}
	}
	if e.CursorLine < 0 {
		e.CursorLine = 0
	}
	if e.CursorLine >= len(e.Lines) {
		e.CursorLine = len(e.Lines) - 1
	}
	lineLen := len([]rune(e.Lines[e.CursorLine]))
	if e.CursorCol < 0 {
		e.CursorCol = 0
	}
	if e.CursorCol > lineLen {
		e.CursorCol = lineLen
	}
}

// Insert adds a rune at cursor position
func (e *EditorState) Insert(r rune) {
	e.clampCursor()
	line := []rune(e.Lines[e.CursorLine])
	line = append(line[:e.CursorCol], append([]rune{r}, line[e.CursorCol:]...)...)
	e.Lines[e.CursorLine] = string(line)
	e.CursorCol++
}

// InsertString adds string at cursor, handling newlines
func (e *EditorState) InsertString(s string) {
	for _, r := range s {
		if r == '\n' {
			e.InsertNewline()
		} else {
			e.Insert(r)
		}
	}
}

// InsertNewline splits current line at cursor
func (e *EditorState) InsertNewline() {
	e.clampCursor()
	runes := []rune(e.Lines[e.CursorLine])

	before := string(runes[:e.CursorCol])
	after := string(runes[e.CursorCol:])

	e.Lines[e.CursorLine] = before
	e.Lines = append(e.Lines[:e.CursorLine+1], append([]string{after}, e.Lines[e.CursorLine+1:]...)...)
	e.CursorLine++
	e.CursorCol = 0
}

// DeleteBackward deletes character before cursor or merges lines
func (e *EditorState) DeleteBackward() bool {
	e.clampCursor()
	if e.CursorCol > 0 {
		line := []rune(e.Lines[e.CursorLine])
		line = append(line[:e.CursorCol-1], line[e.CursorCol:]...)
		e.Lines[e.CursorLine] = string(line)
		e.CursorCol--
		return true
	}
	if e.CursorLine > 0 {
		prevLine := e.Lines[e.CursorLine-1]
		curLine := e.Lines[e.CursorLine]
		newCol := len([]rune(prevLine))
		e.Lines[e.CursorLine-1] = prevLine + curLine
		e.Lines = append(e.Lines[:e.CursorLine], e.Lines[e.CursorLine+1:]...)
		e.CursorLine--
		e.CursorCol = newCol
		return true
	}
	return false
}

// DeleteForward deletes character at cursor or merges with next line
func (e *EditorState) DeleteForward() bool {
	e.clampCursor()
	line := []rune(e.Lines[e.CursorLine])
	if e.CursorCol < len(line) {
		line = append(line[:e.CursorCol], line[e.CursorCol+1:]...)
		e.Lines[e.CursorLine] = string(line)
		return true
	}
	if e.CursorLine < len(e.Lines)-1 {
		e.Lines[e.CursorLine] = e.Lines[e.CursorLine] + e.Lines[e.CursorLine+1]
		e.Lines = append(e.Lines[:e.CursorLine+1], e.Lines[e.CursorLine+2:]...)
		return true
	}
	return false
}

// DeleteToEndOfLine deletes from cursor to end of line
func (e *EditorState) DeleteToEndOfLine() bool {
	e.clampCursor()
	line := []rune(e.Lines[e.CursorLine])
	if e.CursorCol < len(line) {
		e.Lines[e.CursorLine] = string(line[:e.CursorCol])
		return true
	}
	// At end of line - merge with next
	return e.DeleteForward()
}

// DeleteToStartOfLine deletes from start to cursor
func (e *EditorState) DeleteToStartOfLine() bool {
	e.clampCursor()
	if e.CursorCol > 0 {
		line := []rune(e.Lines[e.CursorLine])
		e.Lines[e.CursorLine] = string(line[e.CursorCol:])
		e.CursorCol = 0
		return true
	}
	return false
}

// DeleteWordBackward deletes word before cursor
func (e *EditorState) DeleteWordBackward() bool {
	e.clampCursor()
	if e.CursorCol == 0 {
		return e.DeleteBackward()
	}

	line := []rune(e.Lines[e.CursorLine])
	end := e.CursorCol

	// Skip trailing non-word chars
	for end > 0 && !isWordChar(line[end-1]) {
		end--
	}
	// Skip word chars
	start := end
	for start > 0 && isWordChar(line[start-1]) {
		start--
	}
	if start == e.CursorCol {
		start = e.CursorCol - 1
	}

	line = append(line[:start], line[e.CursorCol:]...)
	e.Lines[e.CursorLine] = string(line)
	e.CursorCol = start
	return true
}

// DeleteWordForward deletes word after cursor
func (e *EditorState) DeleteWordForward() bool {
	e.clampCursor()
	line := []rune(e.Lines[e.CursorLine])
	if e.CursorCol >= len(line) {
		return e.DeleteForward()
	}

	start := e.CursorCol
	end := start

	// Skip word chars
	for end < len(line) && isWordChar(line[end]) {
		end++
	}
	// Skip trailing non-word chars
	for end < len(line) && !isWordChar(line[end]) {
		end++
	}
	if end == start {
		end = start + 1
	}

	line = append(line[:start], line[end:]...)
	e.Lines[e.CursorLine] = string(line)
	return true
}

// DeleteLine removes current line
func (e *EditorState) DeleteLine() bool {
	if len(e.Lines) == 1 {
		e.Lines[0] = ""
		e.CursorCol = 0
		return true
	}
	e.Lines = append(e.Lines[:e.CursorLine], e.Lines[e.CursorLine+1:]...)
	e.clampCursor()
	return true
}

// Navigation

// MoveUp moves cursor to previous line
func (e *EditorState) MoveUp() {
	if e.CursorLine > 0 {
		e.CursorLine--
		e.clampCursor()
	}
}

// MoveDown moves cursor to next line
func (e *EditorState) MoveDown() {
	if e.CursorLine < len(e.Lines)-1 {
		e.CursorLine++
		e.clampCursor()
	}
}

// MoveLeft moves cursor left, wrapping to previous line
func (e *EditorState) MoveLeft() {
	if e.CursorCol > 0 {
		e.CursorCol--
	} else if e.CursorLine > 0 {
		e.CursorLine--
		e.CursorCol = len([]rune(e.Lines[e.CursorLine]))
	}
}

// MoveRight moves cursor right, wrapping to next line
func (e *EditorState) MoveRight() {
	lineLen := len([]rune(e.Lines[e.CursorLine]))
	if e.CursorCol < lineLen {
		e.CursorCol++
	} else if e.CursorLine < len(e.Lines)-1 {
		e.CursorLine++
		e.CursorCol = 0
	}
}

// MoveWordLeft moves cursor to previous word boundary
func (e *EditorState) MoveWordLeft() {
	if e.CursorCol == 0 {
		if e.CursorLine > 0 {
			e.CursorLine--
			e.CursorCol = len([]rune(e.Lines[e.CursorLine]))
		}
		return
	}

	line := []rune(e.Lines[e.CursorLine])
	for e.CursorCol > 0 && !isWordChar(line[e.CursorCol-1]) {
		e.CursorCol--
	}
	for e.CursorCol > 0 && isWordChar(line[e.CursorCol-1]) {
		e.CursorCol--
	}
}

// MoveWordRight moves cursor to next word boundary
func (e *EditorState) MoveWordRight() {
	line := []rune(e.Lines[e.CursorLine])
	lineLen := len(line)

	if e.CursorCol >= lineLen {
		if e.CursorLine < len(e.Lines)-1 {
			e.CursorLine++
			e.CursorCol = 0
		}
		return
	}

	for e.CursorCol < lineLen && isWordChar(line[e.CursorCol]) {
		e.CursorCol++
	}
	for e.CursorCol < lineLen && !isWordChar(line[e.CursorCol]) {
		e.CursorCol++
	}
}

// MoveToLineStart moves cursor to start of line
func (e *EditorState) MoveToLineStart() {
	e.CursorCol = 0
}

// MoveToLineEnd moves cursor to end of line
func (e *EditorState) MoveToLineEnd() {
	e.CursorCol = len([]rune(e.Lines[e.CursorLine]))
}

// MoveToStart moves cursor to start of document
func (e *EditorState) MoveToStart() {
	e.CursorLine = 0
	e.CursorCol = 0
}

// MoveToEnd moves cursor to end of document
func (e *EditorState) MoveToEnd() {
	e.CursorLine = len(e.Lines) - 1
	e.CursorCol = len([]rune(e.Lines[e.CursorLine]))
}

// PageUp moves cursor up by half viewport
func (e *EditorState) PageUp() {
	delta := e.ViewportH / 2
	if delta < 1 {
		delta = 1
	}
	e.CursorLine -= delta
	if e.CursorLine < 0 {
		e.CursorLine = 0
	}
	e.clampCursor()
}

// PageDown moves cursor down by half viewport
func (e *EditorState) PageDown() {
	delta := e.ViewportH / 2
	if delta < 1 {
		delta = 1
	}
	e.CursorLine += delta
	if e.CursorLine >= len(e.Lines) {
		e.CursorLine = len(e.Lines) - 1
	}
	e.clampCursor()
}

// AdjustScroll updates scroll to keep cursor visible
func (e *EditorState) AdjustScroll(viewportW, viewportH int) {
	e.ViewportW = viewportW
	e.ViewportH = viewportH

	// Vertical
	if e.CursorLine < e.ScrollY {
		e.ScrollY = e.CursorLine
	}
	if e.CursorLine >= e.ScrollY+viewportH {
		e.ScrollY = e.CursorLine - viewportH + 1
	}
	if e.ScrollY < 0 {
		e.ScrollY = 0
	}

	// Horizontal
	if e.CursorCol < e.ScrollX {
		e.ScrollX = e.CursorCol
	}
	if e.CursorCol >= e.ScrollX+viewportW {
		e.ScrollX = e.CursorCol - viewportW + 1
	}
	if e.ScrollX < 0 {
		e.ScrollX = 0
	}
}

// HandleKey processes keyboard input, returns true if state changed
func (e *EditorState) HandleKey(key terminal.Key, r rune, mod terminal.Modifier) bool {
	switch key {
	case terminal.KeyUp:
		e.MoveUp()
		return true
	case terminal.KeyDown:
		e.MoveDown()
		return true
	case terminal.KeyLeft:
		if mod&terminal.ModCtrl != 0 {
			e.MoveWordLeft()
		} else {
			e.MoveLeft()
		}
		return true
	case terminal.KeyRight:
		if mod&terminal.ModCtrl != 0 {
			e.MoveWordRight()
		} else {
			e.MoveRight()
		}
		return true
	case terminal.KeyHome:
		if mod&terminal.ModCtrl != 0 {
			e.MoveToStart()
		} else {
			e.MoveToLineStart()
		}
		return true
	case terminal.KeyEnd:
		if mod&terminal.ModCtrl != 0 {
			e.MoveToEnd()
		} else {
			e.MoveToLineEnd()
		}
		return true
	case terminal.KeyPageUp:
		e.PageUp()
		return true
	case terminal.KeyPageDown:
		e.PageDown()
		return true
	case terminal.KeyEnter:
		e.InsertNewline()
		return true
	case terminal.KeyBackspace:
		if mod&terminal.ModCtrl != 0 {
			return e.DeleteWordBackward()
		}
		return e.DeleteBackward()
	case terminal.KeyDelete:
		if mod&terminal.ModCtrl != 0 {
			return e.DeleteWordForward()
		}
		return e.DeleteForward()
	case terminal.KeyCtrlA:
		e.MoveToLineStart()
		return true
	case terminal.KeyCtrlE:
		e.MoveToLineEnd()
		return true
	case terminal.KeyCtrlK:
		return e.DeleteToEndOfLine()
	case terminal.KeyCtrlU:
		return e.DeleteToStartOfLine()
	case terminal.KeyCtrlW:
		return e.DeleteWordBackward()
	case terminal.KeyRune:
		if r >= 32 {
			e.Insert(r)
			return true
		}
	}
	return false
}

// EditorOpts configures editor rendering
type EditorOpts struct {
	LineNumbers  bool
	LineNumWidth int // 0 = auto-size
	WrapLines    bool
	Border       LineType
	Focused      bool
	Style        EditorStyle
}

// EditorStyle defines editor colors
type EditorStyle struct {
	TextFg        terminal.RGB
	TextBg        terminal.RGB
	CursorFg      terminal.RGB
	CursorBg      terminal.RGB
	LineNumFg     terminal.RGB
	LineNumBg     terminal.RGB
	CurrentLineBg terminal.RGB
	BorderFg      terminal.RGB
}

// DefaultEditorStyle returns default colors
func DefaultEditorStyle() EditorStyle {
	return EditorStyle{
		TextFg:        terminal.RGB{R: 220, G: 220, B: 220},
		TextBg:        terminal.RGB{R: 25, G: 25, B: 35},
		CursorFg:      terminal.RGB{R: 0, G: 0, B: 0},
		CursorBg:      terminal.RGB{R: 200, G: 200, B: 200},
		LineNumFg:     terminal.RGB{R: 100, G: 100, B: 120},
		LineNumBg:     terminal.RGB{R: 30, G: 30, B: 40},
		CurrentLineBg: terminal.RGB{R: 35, G: 35, B: 50},
		BorderFg:      terminal.RGB{R: 80, G: 80, B: 100},
	}
}

// Editor renders multi-line editor and returns content height used
func (r Region) Editor(state *EditorState, opts EditorOpts) int {
	if r.W < 3 || r.H < 1 {
		return 0
	}

	style := opts.Style
	if style == (EditorStyle{}) {
		style = DefaultEditorStyle()
	}

	// Calculate content area accounting for border
	contentX := 0
	contentY := 0
	contentW := r.W
	contentH := r.H

	if opts.Border != LineNone {
		if r.H < 3 {
			return 0
		}
		r.Box(opts.Border, style.BorderFg)
		contentX = 1
		contentY = 1
		contentW = r.W - 2
		contentH = r.H - 2
	}

	// Line number gutter
	gutterW := 0
	if opts.LineNumbers {
		gutterW = opts.LineNumWidth
		if gutterW == 0 {
			digits := 1
			n := len(state.Lines)
			for n >= 10 {
				digits++
				n /= 10
			}
			gutterW = digits + 1
		}
		contentW -= gutterW
	}

	if contentW < 1 || contentH < 1 {
		return 0
	}

	state.AdjustScroll(contentW, contentH)

	// Render each visible line
	for y := 0; y < contentH; y++ {
		lineIdx := state.ScrollY + y
		isCurrentLine := lineIdx == state.CursorLine

		bg := style.TextBg
		if isCurrentLine && opts.Focused {
			bg = style.CurrentLineBg
		}

		// Line number gutter
		if opts.LineNumbers {
			for gx := 0; gx < gutterW-1; gx++ {
				r.Cell(contentX+gx, contentY+y, ' ', style.LineNumFg, style.LineNumBg, terminal.AttrNone)
			}
			r.Cell(contentX+gutterW-1, contentY+y, '│', style.LineNumFg, style.LineNumBg, terminal.AttrDim)

			if lineIdx < len(state.Lines) {
				numStr := formatLineNum(lineIdx+1, gutterW-1)
				for i, ch := range numStr {
					r.Cell(contentX+i, contentY+y, ch, style.LineNumFg, style.LineNumBg, terminal.AttrNone)
				}
			}
		}

		textX := contentX + gutterW

		// Fill text area with background
		for x := 0; x < contentW; x++ {
			r.Cell(textX+x, contentY+y, ' ', style.TextFg, bg, terminal.AttrNone)
		}

		if lineIdx >= len(state.Lines) {
			continue
		}

		line := []rune(state.Lines[lineIdx])

		// Scroll indicator left
		if state.ScrollX > 0 && len(line) > 0 {
			r.Cell(textX, contentY+y, '◀', style.LineNumFg, bg, terminal.AttrDim)
		}

		// Render visible text
		for x := 0; x < contentW; x++ {
			charIdx := state.ScrollX + x
			if charIdx >= len(line) {
				break
			}

			ch := line[charIdx]
			fg := style.TextFg
			cellBg := bg

			if opts.Focused && lineIdx == state.CursorLine && charIdx == state.CursorCol {
				fg = style.CursorFg
				cellBg = style.CursorBg
			}

			r.Cell(textX+x, contentY+y, ch, fg, cellBg, terminal.AttrNone)
		}

		// Scroll indicator right
		if state.ScrollX+contentW < len(line) {
			r.Cell(textX+contentW-1, contentY+y, '▶', style.LineNumFg, bg, terminal.AttrDim)
		}

		// Cursor at end of line
		if opts.Focused && lineIdx == state.CursorLine && state.CursorCol >= len(line) {
			cursorX := state.CursorCol - state.ScrollX
			if cursorX >= 0 && cursorX < contentW {
				r.Cell(textX+cursorX, contentY+y, ' ', style.CursorFg, style.CursorBg, terminal.AttrNone)
			}
		}
	}

	// Vertical scroll indicators
	if state.ScrollY > 0 {
		r.Cell(contentX+gutterW+contentW-1, contentY, '▲', style.LineNumFg, style.TextBg, terminal.AttrDim)
	}
	if state.ScrollY+contentH < len(state.Lines) {
		r.Cell(contentX+gutterW+contentW-1, contentY+contentH-1, '▼', style.LineNumFg, style.TextBg, terminal.AttrDim)
	}

	if opts.Border != LineNone {
		return r.H
	}
	return contentH
}

func formatLineNum(num, width int) string {
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