package tui

import "github.com/lixenwraith/vi-fighter/terminal"

// LineType specifies box drawing character style
type LineType uint8

const (
	LineSingle  LineType = iota // ┌─┐│└┘
	LineDouble                  // ╔═╗║╚╝
	LineRounded                 // ╭─╮│╰╯
	LineHeavy                   // ┏━┓┃┗┛
	LineNone                    // spaces (invisible border with padding)
)

// Box drawing character sets indexed by LineType
var boxChars = [...][6]rune{
	LineSingle:  {'┌', '─', '┐', '│', '└', '┘'},
	LineDouble:  {'╔', '═', '╗', '║', '╚', '╝'},
	LineRounded: {'╭', '─', '╮', '│', '╰', '╯'},
	LineHeavy:   {'┏', '━', '┓', '┃', '┗', '┛'},
	LineNone:    {' ', ' ', ' ', ' ', ' ', ' '},
}

const (
	boxTL = 0 // top-left
	boxH  = 1 // horizontal
	boxTR = 2 // top-right
	boxV  = 3 // vertical
	boxBL = 4 // bottom-left
	boxBR = 5 // bottom-right
)

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

// KeyValue renders right-aligned key, separator, left-aligned value on row
// Key width auto-sizes based on content, capped at 40% of region width
// Value gets remainder, minimum 30% of region width
func (r Region) KeyValue(y int, key, value string, keyStyle, valStyle Style, sep rune) {
	if y < 0 || y >= r.H || r.W < 3 {
		return
	}

	keyLen := RuneLen(key)

	// Dynamic allocation: key gets what it needs up to 40%
	maxKeyW := (r.W * 2) / 5  // 40%
	minValW := (r.W * 3) / 10 // 30%

	keyW := keyLen
	if keyW > maxKeyW {
		keyW = maxKeyW
	}
	if keyW < 1 {
		keyW = 1
	}

	valW := r.W - keyW - 1 // -1 for separator
	if valW < minValW && r.W > minValW+2 {
		// Reclaim from key to meet minimum value width
		valW = minValW
		keyW = r.W - valW - 1
		if keyW < 1 {
			keyW = 1
			valW = r.W - 2
		}
	}
	if valW < 1 {
		valW = 1
	}

	// Truncate key if needed
	keyRunes := []rune(key)
	if len(keyRunes) > keyW {
		if keyW > 1 {
			keyRunes = keyRunes[:keyW-1]
			keyRunes = append(keyRunes, '…')
		} else {
			keyRunes = keyRunes[:1]
		}
	}

	// Truncate value if needed
	valRunes := []rune(value)
	if len(valRunes) > valW {
		if valW > 1 {
			valRunes = valRunes[:valW-1]
			valRunes = append(valRunes, '…')
		} else {
			valRunes = valRunes[:1]
		}
	}

	// Right-align key within allocated width
	keyX := keyW - len(keyRunes)
	for i, ch := range keyRunes {
		r.Cell(keyX+i, y, ch, keyStyle.Fg, keyStyle.Bg, keyStyle.Attr)
	}

	// Separator
	r.Cell(keyW, y, sep, keyStyle.Fg, keyStyle.Bg, terminal.AttrDim)

	// Left-align value
	for i, ch := range valRunes {
		r.Cell(keyW+1+i, y, ch, valStyle.Fg, valStyle.Bg, valStyle.Attr)
	}
}

// KeyValueWrap renders key-value with value wrapping to subsequent lines
// Returns number of lines used
// Layout:
//
//	key: value text that is
//	     long and wraps to
//	     next line
func (r Region) KeyValueWrap(y int, key, value string, keyStyle, valStyle Style, sep rune) int {
	if y < 0 || y >= r.H || r.W < 3 {
		return 0
	}

	keyLen := RuneLen(key)

	// Dynamic allocation same as KeyValue
	maxKeyW := (r.W * 2) / 5  // 40%
	minValW := (r.W * 3) / 10 // 30%

	keyW := keyLen
	if keyW > maxKeyW {
		keyW = maxKeyW
	}
	if keyW < 1 {
		keyW = 1
	}

	valW := r.W - keyW - 1 // -1 for separator
	if valW < minValW && r.W > minValW+2 {
		valW = minValW
		keyW = r.W - valW - 1
		if keyW < 1 {
			keyW = 1
			valW = r.W - 2
		}
	}
	if valW < 1 {
		valW = 1
	}

	// Truncate key if needed
	keyRunes := []rune(key)
	if len(keyRunes) > keyW {
		if keyW > 1 {
			keyRunes = keyRunes[:keyW-1]
			keyRunes = append(keyRunes, '…')
		} else {
			keyRunes = keyRunes[:1]
		}
	}

	// Right-align key within allocated width
	keyX := keyW - len(keyRunes)
	for i, ch := range keyRunes {
		r.Cell(keyX+i, y, ch, keyStyle.Fg, keyStyle.Bg, keyStyle.Attr)
	}

	// Separator
	r.Cell(keyW, y, sep, keyStyle.Fg, keyStyle.Bg, terminal.AttrDim)

	// Wrap value text
	valueX := keyW + 1
	lines := WrapText(value, valW)
	if len(lines) == 0 {
		return 1
	}

	rendered := 0
	for i, line := range lines {
		lineY := y + i
		if lineY >= r.H {
			break
		}
		r.Text(valueX, lineY, line, valStyle.Fg, valStyle.Bg, valStyle.Attr)
		rendered++
	}

	if rendered < 1 {
		rendered = 1
	}
	return rendered
}

// MeasureKeyValueWrap calculates lines needed for KeyValueWrap without rendering
// Useful for layout pre-calculation
func (r Region) MeasureKeyValueWrap(key, value string) int {
	if r.W < 3 {
		return 1
	}

	keyLen := RuneLen(key)
	maxKeyW := (r.W * 2) / 5
	minValW := (r.W * 3) / 10

	keyW := keyLen
	if keyW > maxKeyW {
		keyW = maxKeyW
	}
	if keyW < 1 {
		keyW = 1
	}

	valW := r.W - keyW - 1
	if valW < minValW && r.W > minValW+2 {
		valW = minValW
		keyW = r.W - valW - 1
		if keyW < 1 {
			keyW = 1
			valW = r.W - 2
		}
	}
	if valW < 1 {
		valW = 1
	}

	lines := WrapText(value, valW)
	if len(lines) == 0 {
		return 1
	}
	return len(lines)
}

// Divider draws horizontal line with optional centered label
func (r Region) Divider(y int, label string, line LineType, fg terminal.RGB) {
	if y < 0 || y >= r.H {
		return
	}
	if line >= LineType(len(boxChars)) {
		line = LineSingle
	}

	hChar := boxChars[line][boxH]

	// Fill with horizontal line
	for x := 0; x < r.W; x++ {
		r.Cell(x, y, hChar, fg, terminal.RGB{}, terminal.AttrNone)
	}

	// Center label if provided
	if label != "" && r.W > 4 {
		text := " " + label + " "
		textLen := RuneLen(text)
		if textLen > r.W-2 {
			text = Truncate(text, r.W-2)
			textLen = RuneLen(text)
		}
		startX := (r.W - textLen) / 2
		for i, ch := range text {
			r.Cell(startX+i, y, ch, fg, terminal.RGB{}, terminal.AttrBold)
		}
	}
}

// BoxFilled draws border and fills interior with background
func (r Region) BoxFilled(line LineType, fg, bg terminal.RGB) {
	// Fill interior first
	for y := 1; y < r.H-1; y++ {
		for x := 1; x < r.W-1; x++ {
			r.Cell(x, y, ' ', fg, bg, terminal.AttrNone)
		}
	}
	// Draw border on top
	r.Box(line, fg)
}

// Box draws border around region edge
func (r Region) Box(line LineType, fg terminal.RGB) {
	if r.W < 2 || r.H < 2 {
		return
	}
	if line >= LineType(len(boxChars)) {
		line = LineSingle
	}

	chars := boxChars[line]
	bg := terminal.RGB{} // Transparent (use existing bg)

	// Corners
	r.Cell(0, 0, chars[boxTL], fg, bg, terminal.AttrNone)
	r.Cell(r.W-1, 0, chars[boxTR], fg, bg, terminal.AttrNone)
	r.Cell(0, r.H-1, chars[boxBL], fg, bg, terminal.AttrNone)
	r.Cell(r.W-1, r.H-1, chars[boxBR], fg, bg, terminal.AttrNone)

	// Horizontal edges
	for x := 1; x < r.W-1; x++ {
		r.Cell(x, 0, chars[boxH], fg, bg, terminal.AttrNone)
		r.Cell(x, r.H-1, chars[boxH], fg, bg, terminal.AttrNone)
	}

	// Vertical edges
	for y := 1; y < r.H-1; y++ {
		r.Cell(0, y, chars[boxV], fg, bg, terminal.AttrNone)
		r.Cell(r.W-1, y, chars[boxV], fg, bg, terminal.AttrNone)
	}
}

// Card draws titled border and returns inner content region
func (r Region) Card(title string, line LineType, fg terminal.RGB) Region {
	r.Box(line, fg)

	if title != "" && r.W > 4 {
		maxTitleLen := r.W - 4
		displayTitle := title
		if RuneLen(displayTitle) > maxTitleLen {
			displayTitle = Truncate(displayTitle, maxTitleLen)
		}
		titleX := (r.W - RuneLen(displayTitle) - 2) / 2
		r.Text(titleX, 0, " "+displayTitle+" ", fg, terminal.RGB{}, terminal.AttrBold)
	}

	return r.Inset(1)
}

// HLine draws horizontal line across region width at row y
func (r Region) HLine(y int, line LineType, fg terminal.RGB) {
	if y < 0 || y >= r.H {
		return
	}
	if line >= LineType(len(boxChars)) {
		line = LineSingle
	}
	ch := boxChars[line][boxH]
	for x := 0; x < r.W; x++ {
		r.Cell(x, y, ch, fg, terminal.RGB{}, terminal.AttrNone)
	}
}

// VLine draws vertical line across region height at column x
func (r Region) VLine(x int, line LineType, fg terminal.RGB) {
	if x < 0 || x >= r.W {
		return
	}
	if line >= LineType(len(boxChars)) {
		line = LineSingle
	}
	ch := boxChars[line][boxV]
	for y := 0; y < r.H; y++ {
		r.Cell(x, y, ch, fg, terminal.RGB{}, terminal.AttrNone)
	}
}

// Progress draws horizontal progress bar (0.0-1.0)
func (r Region) Progress(x, y, w int, pct float64, fg, bg terminal.RGB) {
	if y < 0 || y >= r.H || w <= 0 {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}

	filled := int(float64(w) * pct)
	remainder := float64(w)*pct - float64(filled)

	for i := 0; i < w; i++ {
		if x+i >= r.W {
			break
		}
		var ch rune
		if i < filled {
			ch = progressFull
		} else if i == filled && remainder >= 0.5 {
			ch = progressHalf
		} else {
			ch = progressEmpty
		}
		r.Cell(x+i, y, ch, fg, bg, terminal.AttrNone)
	}
}

// ProgressV draws vertical progress bar (fills bottom-up)
func (r Region) ProgressV(x, y, h int, pct float64, fg, bg terminal.RGB) {
	if x < 0 || x >= r.W || h <= 0 {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}

	filled := int(float64(h) * pct)

	for i := 0; i < h; i++ {
		if y+i >= r.H {
			break
		}
		var ch rune
		// Fill from bottom up
		if h-1-i < filled {
			ch = progressFull
		} else {
			ch = progressEmpty
		}
		r.Cell(x, y+i, ch, fg, bg, terminal.AttrNone)
	}
}

// Spinner draws spinner character based on frame counter
func (r Region) Spinner(x, y int, frame int, fg terminal.RGB) {
	if x < 0 || x >= r.W || y < 0 || y >= r.H {
		return
	}
	idx := frame % len(spinnerFrames)
	if idx < 0 {
		idx = -idx
	}
	r.Cell(x, y, spinnerFrames[idx], fg, terminal.RGB{}, terminal.AttrNone)
}

// Gauge draws labeled gauge with percentage
func (r Region) Gauge(x, y, w int, value, max int, fg, bg terminal.RGB) {
	if w < 5 || y < 0 || y >= r.H {
		return
	}

	var pct float64
	if max > 0 {
		pct = float64(value) / float64(max)
	}
	if pct > 1 {
		pct = 1
	}
	if pct < 0 {
		pct = 0
	}

	// Format: [████░░░░] 75%
	labelW := 5 // " XXX%" or " 100%"
	barW := w - labelW - 2
	if barW < 1 {
		barW = 1
	}

	r.Cell(x, y, '[', fg, bg, terminal.AttrNone)
	r.Progress(x+1, y, barW, pct, fg, bg)
	r.Cell(x+1+barW, y, ']', fg, bg, terminal.AttrNone)

	pctInt := int(pct * 100)
	var label string
	if pctInt >= 100 {
		label = " 100%"
	} else if pctInt >= 10 {
		label = " " + string(rune('0'+pctInt/10)) + string(rune('0'+pctInt%10)) + "%"
	} else {
		label = "  " + string(rune('0'+pctInt)) + "%"
	}
	r.Text(x+2+barW, y, label, fg, bg, terminal.AttrNone)
}

// CheckState represents checkbox visual state
type CheckState uint8

const (
	CheckNone    CheckState = iota // [ ]
	CheckPartial                   // [o]
	CheckFull                      // [x]
	CheckPlus                      // [+]
)

// Checkbox draws a checkbox indicator
func (r Region) Checkbox(x, y int, state CheckState, fg terminal.RGB) {
	if x < 0 || x+2 >= r.W || y < 0 || y >= r.H {
		return
	}
	var ch rune
	switch state {
	case CheckNone:
		ch = ' '
	case CheckPartial:
		ch = 'o'
	case CheckFull:
		ch = 'x'
	case CheckPlus:
		ch = '+'
	}
	r.Cell(x, y, '[', fg, terminal.RGB{}, terminal.AttrNone)
	r.Cell(x+1, y, ch, fg, terminal.RGB{}, terminal.AttrNone)
	r.Cell(x+2, y, ']', fg, terminal.RGB{}, terminal.AttrNone)
}