package tui

import (
	"unicode"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// TextFieldState holds editable text field state
type TextFieldState struct {
	Text   []rune
	Cursor int // Position before which cursor sits (0 = before first char)
	Scroll int // First visible rune index
}

// NewTextFieldState creates initialized text field state
func NewTextFieldState(initial string) *TextFieldState {
	runes := []rune(initial)
	return &TextFieldState{
		Text:   runes,
		Cursor: len(runes),
		Scroll: 0,
	}
}

// Value returns current text as string
func (t *TextFieldState) Value() string {
	return string(t.Text)
}

// SetValue replaces text and moves cursor to end
func (t *TextFieldState) SetValue(s string) {
	t.Text = []rune(s)
	t.Cursor = len(t.Text)
	t.Scroll = 0
}

// Clear empties the field
func (t *TextFieldState) Clear() {
	t.Text = nil
	t.Cursor = 0
	t.Scroll = 0
}

// Insert adds rune at cursor position
func (t *TextFieldState) Insert(r rune) {
	t.Text = append(t.Text[:t.Cursor], append([]rune{r}, t.Text[t.Cursor:]...)...)
	t.Cursor++
}

// InsertString adds string at cursor position
func (t *TextFieldState) InsertString(s string) {
	runes := []rune(s)
	t.Text = append(t.Text[:t.Cursor], append(runes, t.Text[t.Cursor:]...)...)
	t.Cursor += len(runes)
}

// DeleteBackward removes rune before cursor
func (t *TextFieldState) DeleteBackward() bool {
	if t.Cursor > 0 {
		t.Text = append(t.Text[:t.Cursor-1], t.Text[t.Cursor:]...)
		t.Cursor--
		return true
	}
	return false
}

// DeleteForward removes rune at cursor
func (t *TextFieldState) DeleteForward() bool {
	if t.Cursor < len(t.Text) {
		t.Text = append(t.Text[:t.Cursor], t.Text[t.Cursor+1:]...)
		return true
	}
	return false
}

// DeleteWordBackward removes word before cursor
func (t *TextFieldState) DeleteWordBackward() bool {
	if t.Cursor == 0 {
		return false
	}
	// Skip trailing non-word chars
	end := t.Cursor
	for end > 0 && !isWordChar(t.Text[end-1]) {
		end--
	}
	// Skip word chars
	start := end
	for start > 0 && isWordChar(t.Text[start-1]) {
		start--
	}
	if start == t.Cursor {
		start = t.Cursor - 1
	}
	t.Text = append(t.Text[:start], t.Text[t.Cursor:]...)
	t.Cursor = start
	return true
}

// DeleteWordForward removes word after cursor
func (t *TextFieldState) DeleteWordForward() bool {
	if t.Cursor >= len(t.Text) {
		return false
	}
	// Skip word chars
	end := t.Cursor
	for end < len(t.Text) && isWordChar(t.Text[end]) {
		end++
	}
	// Skip trailing non-word chars
	for end < len(t.Text) && !isWordChar(t.Text[end]) {
		end++
	}
	if end == t.Cursor {
		end = t.Cursor + 1
	}
	t.Text = append(t.Text[:t.Cursor], t.Text[end:]...)
	return true
}

// DeleteToEnd removes from cursor to end
func (t *TextFieldState) DeleteToEnd() bool {
	if t.Cursor < len(t.Text) {
		t.Text = t.Text[:t.Cursor]
		return true
	}
	return false
}

// DeleteToStart removes from start to cursor
func (t *TextFieldState) DeleteToStart() bool {
	if t.Cursor > 0 {
		t.Text = t.Text[t.Cursor:]
		t.Cursor = 0
		t.Scroll = 0
		return true
	}
	return false
}

// MoveLeft moves cursor left
func (t *TextFieldState) MoveLeft() {
	if t.Cursor > 0 {
		t.Cursor--
	}
}

// MoveRight moves cursor right
func (t *TextFieldState) MoveRight() {
	if t.Cursor < len(t.Text) {
		t.Cursor++
	}
}

// MoveWordLeft moves cursor to previous word boundary
func (t *TextFieldState) MoveWordLeft() {
	if t.Cursor == 0 {
		return
	}
	// Skip non-word chars
	for t.Cursor > 0 && !isWordChar(t.Text[t.Cursor-1]) {
		t.Cursor--
	}
	// Skip word chars
	for t.Cursor > 0 && isWordChar(t.Text[t.Cursor-1]) {
		t.Cursor--
	}
}

// MoveWordRight moves cursor to next word boundary
func (t *TextFieldState) MoveWordRight() {
	if t.Cursor >= len(t.Text) {
		return
	}
	// Skip word chars
	for t.Cursor < len(t.Text) && isWordChar(t.Text[t.Cursor]) {
		t.Cursor++
	}
	// Skip non-word chars
	for t.Cursor < len(t.Text) && !isWordChar(t.Text[t.Cursor]) {
		t.Cursor++
	}
}

// MoveToStart moves cursor to beginning
func (t *TextFieldState) MoveToStart() {
	t.Cursor = 0
}

// MoveToEnd moves cursor to end
func (t *TextFieldState) MoveToEnd() {
	t.Cursor = len(t.Text)
}

// AdjustScroll updates scroll to keep cursor visible within viewport width
func (t *TextFieldState) AdjustScroll(viewportW int) {
	if viewportW <= 0 {
		return
	}
	if t.Cursor < t.Scroll {
		t.Scroll = t.Cursor
	}
	if t.Cursor >= t.Scroll+viewportW {
		t.Scroll = t.Cursor - viewportW + 1
	}
	if t.Scroll < 0 {
		t.Scroll = 0
	}
}

// HandleKey processes keyboard input, returns true if state changed
func (t *TextFieldState) HandleKey(key terminal.Key, r rune, mod terminal.Modifier) bool {
	switch key {
	case terminal.KeyLeft:
		if mod&terminal.ModCtrl != 0 {
			t.MoveWordLeft()
		} else {
			t.MoveLeft()
		}
		return true
	case terminal.KeyRight:
		if mod&terminal.ModCtrl != 0 {
			t.MoveWordRight()
		} else {
			t.MoveRight()
		}
		return true
	case terminal.KeyHome, terminal.KeyCtrlA:
		t.MoveToStart()
		return true
	case terminal.KeyEnd, terminal.KeyCtrlE:
		t.MoveToEnd()
		return true
	case terminal.KeyBackspace:
		if mod&terminal.ModCtrl != 0 {
			return t.DeleteWordBackward()
		}
		return t.DeleteBackward()
	case terminal.KeyDelete:
		if mod&terminal.ModCtrl != 0 {
			return t.DeleteWordForward()
		}
		return t.DeleteForward()
	case terminal.KeyCtrlK:
		return t.DeleteToEnd()
	case terminal.KeyCtrlU:
		return t.DeleteToStart()
	case terminal.KeyCtrlW:
		return t.DeleteWordBackward()
	case terminal.KeyRune:
		if r >= 32 { // Printable
			t.Insert(r)
			return true
		}
	}
	return false
}

// isWordChar returns true for word-constituent characters
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// TextFieldOpts configures text field rendering
type TextFieldOpts struct {
	Placeholder string   // Shown when empty
	Prefix      string   // Left prompt (e.g., "> ")
	Mask        rune     // Password mask, 0 = none
	MaxLen      int      // Max runes, 0 = unlimited
	Border      LineType // Border style, LineNone = no border
	Focused     bool     // Show cursor and accept input
	Style       TextFieldStyle
}

// TextFieldStyle defines text field colors
type TextFieldStyle struct {
	TextFg        terminal.RGB
	TextBg        terminal.RGB
	CursorFg      terminal.RGB
	CursorBg      terminal.RGB
	PlaceholderFg terminal.RGB
	PrefixFg      terminal.RGB
	BorderFg      terminal.RGB
}

// DefaultTextFieldStyle returns default colors
func DefaultTextFieldStyle() TextFieldStyle {
	return TextFieldStyle{
		TextFg:        terminal.RGB{R: 220, G: 220, B: 220},
		TextBg:        terminal.RGB{R: 30, G: 30, B: 40},
		CursorFg:      terminal.RGB{R: 0, G: 0, B: 0},
		CursorBg:      terminal.RGB{R: 200, G: 200, B: 200},
		PlaceholderFg: terminal.RGB{R: 100, G: 100, B: 110},
		PrefixFg:      terminal.RGB{R: 150, G: 150, B: 180},
		BorderFg:      terminal.RGB{R: 80, G: 80, B: 100},
	}
}

// TextField renders text field and returns content height used
func (r Region) TextField(state *TextFieldState, opts TextFieldOpts) int {
	if r.W < 3 || r.H < 1 {
		return 0
	}

	style := opts.Style
	if style == (TextFieldStyle{}) {
		style = DefaultTextFieldStyle()
	}

	// Calculate content area
	contentY := 0
	contentX := 0
	contentW := r.W
	contentH := 1

	if opts.Border != LineNone {
		if r.H < 3 {
			return 0
		}
		r.Box(opts.Border, style.BorderFg)
		contentY = 1
		contentX = 1
		contentW = r.W - 2
		contentH = r.H - 2
		if contentH > 1 {
			contentH = 1
		}
	}

	// Fill background
	for x := contentX; x < contentX+contentW; x++ {
		r.Cell(x, contentY, ' ', style.TextFg, style.TextBg, terminal.AttrNone)
	}

	x := contentX

	// Prefix
	if opts.Prefix != "" {
		for _, ch := range opts.Prefix {
			if x >= contentX+contentW {
				break
			}
			r.Cell(x, contentY, ch, style.PrefixFg, style.TextBg, terminal.AttrNone)
			x++
		}
	}

	// Calculate viewport
	viewportW := contentX + contentW - x
	if viewportW < 1 {
		return contentH + 2*boolToInt(opts.Border != LineNone)
	}

	// Adjust scroll
	state.AdjustScroll(viewportW)

	// Render text or placeholder
	text := state.Text
	isEmpty := len(text) == 0

	if isEmpty && opts.Placeholder != "" && !opts.Focused {
		// Placeholder
		placeholder := opts.Placeholder
		if RuneLen(placeholder) > viewportW {
			placeholder = Truncate(placeholder, viewportW)
		}
		for i, ch := range placeholder {
			if x+i >= contentX+contentW {
				break
			}
			r.Cell(x+i, contentY, ch, style.PlaceholderFg, style.TextBg, terminal.AttrDim)
		}
	} else {
		// Scroll indicators
		if state.Scroll > 0 && x > contentX {
			r.Cell(x-1, contentY, '◀', style.PlaceholderFg, style.TextBg, terminal.AttrNone)
		}

		// Text content
		for i := 0; i < viewportW; i++ {
			runeIdx := state.Scroll + i
			ch := ' '
			if runeIdx < len(text) {
				ch = text[runeIdx]
				if opts.Mask != 0 {
					ch = opts.Mask
				}
			}

			fg := style.TextFg
			bg := style.TextBg

			// Cursor highlighting
			if opts.Focused && runeIdx == state.Cursor {
				fg = style.CursorFg
				bg = style.CursorBg
			}

			r.Cell(x+i, contentY, ch, fg, bg, terminal.AttrNone)
		}

		// Cursor at end
		if opts.Focused && state.Cursor == len(text) {
			cursorX := x + state.Cursor - state.Scroll
			if cursorX >= x && cursorX < contentX+contentW {
				r.Cell(cursorX, contentY, ' ', style.CursorFg, style.CursorBg, terminal.AttrNone)
			}
		}

		// Right scroll indicator
		if state.Scroll+viewportW < len(text) {
			r.Cell(contentX+contentW-1, contentY, '▶', style.PlaceholderFg, style.TextBg, terminal.AttrNone)
		}
	}

	if opts.Border != LineNone {
		return 3
	}
	return 1
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}