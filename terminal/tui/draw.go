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

// CheckState represents checkbox visual state
type CheckState uint8

const (
	CheckNone    CheckState = iota // [ ]
	CheckPartial                   // [o]
	CheckFull                      // [x]
	CheckPlus                      // [+]
)