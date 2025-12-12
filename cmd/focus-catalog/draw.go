package main

import (
	"strings"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Box drawing characters - single line
const (
	boxTL = '┌'
	boxTR = '┐'
	boxBL = '└'
	boxBR = '┘'
	boxH  = '─'
	boxV  = '│'
	boxTT = '┬'
	boxBT = '┴'
)

// Box drawing characters - double line
const (
	dboxTL = '╔'
	dboxTR = '╗'
	dboxBL = '╚'
	dboxBR = '╝'
	dboxH  = '═'
	dboxV  = '║'
	dboxLT = '╠'
	dboxRT = '╣'
)

// Connector characters
const (
	arrowDown = '▼'
	arrowUp   = '▲'
	connV     = '│'
	connSplit = '┼'
	starChar  = '★'
)

// drawText renders text string at position with styling.
func drawText(cells []terminal.Cell, width, x, y int, text string, fg, bg terminal.RGB, attr terminal.Attr) {
	for i, r := range text {
		if x+i >= width || x+i < 0 {
			break
		}
		cells[y*width+x+i] = terminal.Cell{
			Rune:  r,
			Fg:    fg,
			Bg:    bg,
			Attrs: attr,
		}
	}
}

// drawRect fills rectangle area with background color.
func drawRect(cells []terminal.Cell, startX, startY, rectW, rectH, totalWidth int, bg terminal.RGB) {
	for row := startY; row < startY+rectH; row++ {
		for col := startX; col < startX+rectW && col < totalWidth; col++ {
			idx := row*totalWidth + col
			if idx >= 0 && idx < len(cells) {
				cells[idx] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
			}
		}
	}
}

// setCell safely sets a single cell with bounds checking.
func setCell(cells []terminal.Cell, totalW, x, y int, r rune, fg terminal.RGB) {
	if x >= 0 && x < totalW && y >= 0 {
		idx := y*totalW + x
		if idx < len(cells) {
			cells[idx] = terminal.Cell{Rune: r, Fg: fg, Bg: colorDefaultBg}
		}
	}
}

// drawSingleBox draws a single-line bordered rectangle.
func drawSingleBox(cells []terminal.Cell, totalW, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}

	setCell(cells, totalW, x, y, boxTL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y, boxTR, colorPaneBorder)
	setCell(cells, totalW, x, y+h-1, boxBL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y+h-1, boxBR, colorPaneBorder)

	for i := 1; i < w-1; i++ {
		setCell(cells, totalW, x+i, y, boxH, colorPaneBorder)
		setCell(cells, totalW, x+i, y+h-1, boxH, colorPaneBorder)
	}

	for i := 1; i < h-1; i++ {
		setCell(cells, totalW, x, y+i, boxV, colorPaneBorder)
		setCell(cells, totalW, x+w-1, y+i, boxV, colorPaneBorder)
	}
}

// drawDoubleBox draws a double-line bordered rectangle.
func drawDoubleBox(cells []terminal.Cell, totalW, x, y, w, h int) {
	if w < 2 || h < 2 {
		return
	}

	setCell(cells, totalW, x, y, dboxTL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y, dboxTR, colorPaneBorder)
	setCell(cells, totalW, x, y+h-1, dboxBL, colorPaneBorder)
	setCell(cells, totalW, x+w-1, y+h-1, dboxBR, colorPaneBorder)

	for i := 1; i < w-1; i++ {
		setCell(cells, totalW, x+i, y, dboxH, colorPaneBorder)
		setCell(cells, totalW, x+i, y+h-1, dboxH, colorPaneBorder)
	}

	for i := 1; i < h-1; i++ {
		setCell(cells, totalW, x, y+i, dboxV, colorPaneBorder)
		setCell(cells, totalW, x+w-1, y+i, dboxV, colorPaneBorder)
	}
}

// drawDoubleFrame draws outer double-line frame.
func drawDoubleFrame(cells []terminal.Cell, totalW, x, y, w, h int) {
	drawDoubleBox(cells, totalW, x, y, w, h)
}

// truncateWithEllipsis shortens string with ellipsis if exceeds maxLen.
func truncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 3 {
		return s[:min(len(s), maxLen)]
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}

// formatDirHints truncates directory list to fit available width.
func formatDirHints(dirs []string, maxLen int) string {
	if len(dirs) == 0 || maxLen < 4 {
		return ""
	}

	var result strings.Builder
	shown := 0
	maxDirs := 3

	for i, d := range dirs {
		if i >= maxDirs {
			break
		}

		addition := d
		if result.Len() > 0 {
			addition = " " + d
		}

		projected := result.Len() + len(addition)
		remaining := len(dirs) - i - 1
		if remaining > 0 && projected+4 > maxLen {
			break
		}
		if remaining == 0 && projected > maxLen {
			break
		}

		result.WriteString(addition)
		shown++
	}

	if shown < len(dirs) {
		if result.Len()+4 <= maxLen {
			result.WriteString(" ...")
		}
	}

	return result.String()
}

// drawColoredTags renders tag string with syntax highlighting.
func drawColoredTags(cells []terminal.Cell, w, x, y int, tagStr string, bg terminal.RGB) int {
	if tagStr == "" || x >= w-1 {
		return x
	}

	maxX := w - 1
	i := 0
	runes := []rune(tagStr)
	n := len(runes)

	for i < n && x < maxX {
		if runes[i] == '#' {
			cells[y*w+x] = terminal.Cell{Rune: '#', Fg: colorGroupNameFg, Bg: bg}
			x++
			i++

			for i < n && x < maxX && runes[i] != '{' && runes[i] != ' ' {
				cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: colorGroupNameFg, Bg: bg}
				x++
				i++
			}

			if i < n && runes[i] == '{' && x < maxX {
				cells[y*w+x] = terminal.Cell{Rune: '{', Fg: colorGroupNameFg, Bg: bg}
				x++
				i++

				for i < n && x < maxX && runes[i] != '}' {
					fg := colorTagNameFg
					if runes[i] == ',' {
						fg = colorGroupNameFg
					}
					cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: fg, Bg: bg}
					x++
					i++
				}

				if i < n && runes[i] == '}' && x < maxX {
					cells[y*w+x] = terminal.Cell{Rune: '}', Fg: colorGroupNameFg, Bg: bg}
					x++
					i++
				}
			}
		} else if runes[i] == ' ' {
			if x < maxX {
				cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: colorDefaultFg, Bg: bg}
				x++
			}
			i++
		} else {
			cells[y*w+x] = terminal.Cell{Rune: runes[i], Fg: colorDefaultFg, Bg: bg}
			x++
			i++
		}
	}

	return x
}