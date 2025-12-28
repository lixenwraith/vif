package tui

import "github.com/lixenwraith/vi-fighter/terminal"

// Progress bar characters
const (
	progressFull  = '█'
	progressEmpty = '░'
	progressHalf  = '▌'
)

// Style bundles foreground, background, and attributes
type Style struct {
	Fg   terminal.RGB
	Bg   terminal.RGB
	Attr terminal.Attr
}

// DefaultStyle returns style with zero values (transparent bg)
func DefaultStyle(fg terminal.RGB) Style {
	return Style{Fg: fg}
}

// IsZero returns true if style has no colors or attributes set
func (s Style) IsZero() bool {
	return s.Fg == (terminal.RGB{}) && s.Bg == (terminal.RGB{}) && s.Attr == terminal.AttrNone
}

// Spinner frames
var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

// Text renders text at position, truncates at region edge
func (r Region) Text(x, y int, s string, fg, bg terminal.RGB, attr terminal.Attr) {
	if y < 0 || y >= r.H {
		return
	}
	col := 0
	for _, ch := range s {
		if x+col >= r.W {
			break
		}
		if x+col >= 0 {
			r.Cell(x+col, y, ch, fg, bg, attr)
		}
		col++
	}
}

// TextRight renders text right-aligned on row
func (r Region) TextRight(y int, s string, fg, bg terminal.RGB, attr terminal.Attr) {
	x := r.W - RuneLen(s)
	r.Text(x, y, s, fg, bg, attr)
}

// TextCenter renders text centered on row
func (r Region) TextCenter(y int, s string, fg, bg terminal.RGB, attr terminal.Attr) {
	x := (r.W - RuneLen(s)) / 2
	r.Text(x, y, s, fg, bg, attr)
}

// TextStyled renders text using Style struct
func (r Region) TextStyled(x, y int, s string, style Style) {
	if y < 0 || y >= r.H {
		return
	}
	col := 0
	for _, ch := range s {
		if x+col >= r.W {
			break
		}
		if x+col >= 0 {
			r.Cell(x+col, y, ch, style.Fg, style.Bg, style.Attr)
		}
		col++
	}
}

// TextBlock renders wrapped text within region bounds
// Returns number of lines rendered (for layout calculations)
func (r Region) TextBlock(x, y int, text string, fg, bg terminal.RGB, attr terminal.Attr) int {
	if x >= r.W || y >= r.H || text == "" {
		return 0
	}

	availW := r.W - x
	if availW < 1 {
		return 0
	}

	lines := WrapText(text, availW)
	rendered := 0

	for i, line := range lines {
		lineY := y + i
		if lineY >= r.H {
			break
		}
		r.Text(x, lineY, line, fg, bg, attr)
		rendered++
	}

	return rendered
}

// TextBlockStyled renders wrapped text using Style struct
// Returns number of lines rendered
func (r Region) TextBlockStyled(x, y int, text string, style Style) int {
	return r.TextBlock(x, y, text, style.Fg, style.Bg, style.Attr)
}

// InputOpts configures single-line input field
type InputOpts struct {
	Label    string
	LabelFg  terminal.RGB
	Text     string
	Cursor   int // Cursor position in text (rune index)
	CursorBg terminal.RGB
	TextFg   terminal.RGB
	Bg       terminal.RGB
}

// Input renders labeled text input field on row y
// Handles cursor display and horizontal scrolling
func (r Region) Input(y int, opts InputOpts) {
	if y < 0 || y >= r.H || r.W < 5 {
		return
	}

	x := 0

	// Label
	if opts.Label != "" {
		for _, ch := range opts.Label {
			if x >= r.W {
				break
			}
			r.Cell(x, y, ch, opts.LabelFg, opts.Bg, terminal.AttrNone)
			x++
		}
	}

	// Available width for input text
	inputW := r.W - x
	if inputW < 3 {
		return
	}

	runes := []rune(opts.Text)
	cursor := opts.Cursor
	if cursor > len(runes) {
		cursor = len(runes)
	}
	if cursor < 0 {
		cursor = 0
	}

	// Horizontal scroll to keep cursor visible
	scroll := 0
	if cursor >= inputW-1 {
		scroll = cursor - inputW + 2
	}

	// Render visible portion
	for i := 0; i < inputW; i++ {
		runeIdx := scroll + i
		ch := ' '
		if runeIdx < len(runes) {
			ch = runes[runeIdx]
		}

		bg := opts.Bg
		if runeIdx == cursor {
			bg = opts.CursorBg
		}

		r.Cell(x+i, y, ch, opts.TextFg, bg, terminal.AttrNone)
	}

	// Cursor at end if past text
	if cursor == len(runes) && cursor-scroll < inputW {
		r.Cell(x+cursor-scroll, y, ' ', opts.TextFg, opts.CursorBg, terminal.AttrNone)
	}
}