package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	"github.com/lixenwraith/vi-fighter/asset"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Editor constants
const (
	GridRows     = 12
	GridCols     = 12
	MinChar      = 32
	MaxChar      = 126
	PreviewLimit = 40
)

// UI Colors
var (
	ColorBg        = terminal.RGB{R: 16, G: 16, B: 20}
	ColorGridBg    = terminal.RGB{R: 30, G: 30, B: 35}
	ColorPixelOn   = terminal.RGB{R: 0, G: 255, B: 150}
	ColorPixelOff  = terminal.RGB{R: 60, G: 60, B: 70}
	ColorCursor    = terminal.RGB{R: 255, G: 0, B: 255}
	ColorText      = terminal.RGB{R: 200, G: 200, B: 220}
	ColorHighlight = terminal.RGB{R: 255, G: 200, B: 0}
	ColorDim       = terminal.RGB{R: 100, G: 100, B: 110}
	ColorBorder    = terminal.RGB{R: 80, G: 80, B: 100}
	ColorSuccess   = terminal.RGB{R: 50, G: 200, B: 100}
	ColorError     = terminal.RGB{R: 200, G: 50, B: 50}
)

// Box drawing characters
const (
	BoxTopLeft     = '┌'
	BoxTopRight    = '┐'
	BoxBottomLeft  = '└'
	BoxBottomRight = '┘'
	BoxHorizontal  = '─'
	BoxVertical    = '│'
	BlockFull      = '█'
	BlockUpper     = '▀'
	BlockLower     = '▄'
	DotMiddle      = '·'
)

type Editor struct {
	term    terminal.Terminal
	running bool
	width   int
	height  int

	// Data
	glyphs   map[rune][GridRows]uint16
	original map[rune][GridRows]uint16
	current  rune
	modified bool

	// Cursor
	cursorX int
	cursorY int

	// UI GameState
	previewText string
	typingMode  bool
	statusMsg   string
	statusType  int // 0=info, 1=success, 2=error
	statusTimer time.Time

	// Clipboard buffer for glyph copy/paste
	clipboard [GridRows]uint16
	hasClip   bool

	// Row clipboard for row operations
	rowClip    uint16
	hasRowClip bool
}

func main() {
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize terminal: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if r := recover(); r != nil {
			terminal.EmergencyReset(os.Stdout)
			fmt.Fprintf(os.Stderr, "CRASH: %v\n%s\n", r, debug.Stack())
		} else {
			term.Fini()
		}
	}()

	editor := NewEditor(term)
	editor.Run()
}

func NewEditor(term terminal.Terminal) *Editor {
	e := &Editor{
		term:        term,
		running:     true,
		glyphs:      make(map[rune][GridRows]uint16),
		original:    make(map[rune][GridRows]uint16),
		current:     'A',
		cursorX:     6,
		cursorY:     5,
		previewText: "ABCDEFG 0123456789",
	}
	e.loadAssets()
	return e
}

func (e *Editor) loadAssets() {
	for i := 0; i < len(asset.SplashFont); i++ {
		r := rune(MinChar + i)
		e.glyphs[r] = asset.SplashFont[i]
		e.original[r] = asset.SplashFont[i]
	}
}

func (e *Editor) Run() {
	w, h := e.term.Size()
	e.width = w
	e.height = h

	e.draw()

	for e.running {
		ev := e.term.PollEvent()

		// Clear expired status
		if !e.statusTimer.IsZero() && time.Now().After(e.statusTimer) {
			e.statusMsg = ""
			e.statusTimer = time.Time{}
		}

		switch ev.Type {
		case terminal.EventResize:
			e.width = ev.Width
			e.height = ev.Height
			e.term.Sync()

		case terminal.EventKey:
			e.handleEvent(ev)

		case terminal.EventClosed, terminal.EventError:
			e.running = false
			continue
		}

		e.draw()
	}
}

func (e *Editor) handleEvent(ev terminal.Event) {
	if e.typingMode {
		e.handleTypingInput(ev)
		return
	}

	switch ev.Key {
	case terminal.KeyCtrlC, terminal.KeyCtrlQ:
		e.running = false
	case terminal.KeyEscape:
		e.running = false

	case terminal.KeyUp:
		e.moveCursor(0, -1)
	case terminal.KeyDown:
		e.moveCursor(0, 1)
	case terminal.KeyLeft:
		e.moveCursor(-1, 0)
	case terminal.KeyRight:
		e.moveCursor(1, 0)

	case terminal.KeySpace:
		e.toggleBit(e.cursorY, e.cursorX)
		e.modified = true
	case terminal.KeyEnter:
		e.setBit(e.cursorY, e.cursorX, true)
		e.modified = true
	case terminal.KeyBackspace, terminal.KeyDelete:
		e.setBit(e.cursorY, e.cursorX, false)
		e.modified = true

	case terminal.KeyRune:
		e.handleRuneInput(ev.Rune)
	}
}

func (e *Editor) handleRuneInput(r rune) {
	switch r {
	// Navigation (WASD + HJKL)
	case 'w', 'k':
		e.moveCursor(0, -1)
	case 's', 'j':
		e.moveCursor(0, 1)
	case 'a', 'h':
		e.moveCursor(-1, 0)
	case 'd', 'l':
		e.moveCursor(1, 0)

	// Fast navigation
	case 'W', 'K':
		e.moveCursor(0, -4)
	case 'S', 'J':
		e.moveCursor(0, 4)
	case 'A', 'H':
		e.moveCursor(-4, 0)
	case 'D', 'L':
		e.moveCursor(4, 0)

	// Home/End style
	case '0':
		e.cursorX = 0
	case '$':
		e.cursorX = GridCols - 1
	case 'g':
		e.cursorY = 0
	case 'G':
		e.cursorY = GridRows - 1

	// Char selection
	case ']':
		if e.current < MaxChar {
			e.current++
			e.modified = false
		}
	case '[':
		if e.current > MinChar {
			e.current--
			e.modified = false
		}

	// Direct char jump
	case '/':
		e.typingMode = true
		e.setStatus("Type character to edit, then ESC", 0)

	// Pixel operations
	case 'x':
		e.setBit(e.cursorY, e.cursorX, false)
		e.modified = true
	case 'o':
		e.setBit(e.cursorY, e.cursorX, true)
		e.modified = true

	// Row operations
	case 'X':
		g := e.glyphs[e.current]
		g[e.cursorY] = 0x0000
		e.glyphs[e.current] = g
		e.modified = true
		e.setStatus("Cleared row", 1)
	case 'F':
		g := e.glyphs[e.current]
		g[e.cursorY] = 0xFFFF
		e.glyphs[e.current] = g
		e.modified = true
		e.setStatus("Filled row", 1)

	// Row clipboard operations
	case 'R':
		e.rowClip = e.glyphs[e.current][e.cursorY]
		e.hasRowClip = true
		e.setStatus(fmt.Sprintf("Yanked row %X", e.cursorY), 1)
	case 'P':
		if e.hasRowClip {
			g := e.glyphs[e.current]
			g[e.cursorY] = e.rowClip
			e.glyphs[e.current] = g
			e.modified = true
			e.setStatus("Pasted row", 1)
		} else {
			e.setStatus("Row buffer empty", 2)
		}
	case 'O':
		e.insertRowAbove()
		e.modified = true
		e.setStatus("Inserted row above", 1)
	case 'N':
		e.insertRowBelow()
		e.modified = true
		e.setStatus("Inserted row below", 1)
	case 'Z':
		e.deleteRow()
		e.modified = true
		e.setStatus("Deleted row", 1)

	// Glyph operations
	case 'c':
		e.glyphs[e.current] = [GridRows]uint16{}
		e.modified = true
		e.setStatus("Cleared glyph", 1)
	case 'i':
		g := e.glyphs[e.current]
		for row := 0; row < GridRows; row++ {
			g[row] = ^g[row]
		}
		e.glyphs[e.current] = g
		e.modified = true
		e.setStatus("Inverted glyph", 1)
	case 'r':
		if orig, ok := e.original[e.current]; ok {
			e.glyphs[e.current] = orig
			e.modified = false
			e.setStatus("Reset to original", 1)
		}

	// Transformations
	case '<':
		e.shiftLeft()
		e.modified = true
		e.setStatus("Shifted left", 1)
	case '>':
		e.shiftRight()
		e.modified = true
		e.setStatus("Shifted right", 1)
	case '^':
		e.shiftUp()
		e.modified = true
		e.setStatus("Shifted up", 1)
	case 'v':
		e.shiftDown()
		e.modified = true
		e.setStatus("Shifted down", 1)
	case '|':
		e.flipHorizontal()
		e.modified = true
		e.setStatus("Flipped horizontal", 1)
	case '_':
		e.flipVertical()
		e.modified = true
		e.setStatus("Flipped vertical", 1)

	// Clipboard
	case 'p':
		if e.hasClip {
			e.glyphs[e.current] = e.clipboard
			e.modified = true
			e.setStatus("Pasted glyph", 1)
		} else {
			e.setStatus("Clipboard empty", 2)
		}
	case 'Y':
		e.clipboard = e.glyphs[e.current]
		e.hasClip = true
		e.setStatus("Copied glyph to buffer", 1)

	// Export
	case 'y':
		e.copyToClipboard()
	case 'E':
		e.exportAllGlyphs()

	// Preview text
	case 't':
		e.typingMode = true
		e.setStatus("TYPING MODE - Edit preview text (ESC to exit)", 0)

	// Quit
	case 'q':
		e.running = false
	}
}

func (e *Editor) insertRowAbove() {
	g := e.glyphs[e.current]
	// Shift rows down from cursor, losing bottom row
	for r := GridRows - 1; r > e.cursorY; r-- {
		g[r] = g[r-1]
	}
	g[e.cursorY] = 0x0000
	e.glyphs[e.current] = g
}

func (e *Editor) insertRowBelow() {
	g := e.glyphs[e.current]
	// Shift rows down from cursor+1, losing bottom row
	for r := GridRows - 1; r > e.cursorY+1; r-- {
		g[r] = g[r-1]
	}
	if e.cursorY+1 < GridRows {
		g[e.cursorY+1] = 0x0000
	}
	e.glyphs[e.current] = g
}

func (e *Editor) deleteRow() {
	g := e.glyphs[e.current]
	// Shift rows up from cursor, bottom becomes empty
	for r := e.cursorY; r < GridRows-1; r++ {
		g[r] = g[r+1]
	}
	g[GridRows-1] = 0x0000
	e.glyphs[e.current] = g
}

func (e *Editor) handleTypingInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		e.typingMode = false
		e.setStatus("Editor mode", 0)
	case terminal.KeyEnter:
		e.typingMode = false
		e.setStatus("Editor mode", 0)
	case terminal.KeyBackspace, terminal.KeyDelete:
		if len(e.previewText) > 0 {
			e.previewText = e.previewText[:len(e.previewText)-1]
		}
	case terminal.KeyRune:
		r := ev.Rune
		// Direct char jump mode check
		if e.statusMsg == "Type character to edit, then ESC" {
			if r >= MinChar && r <= MaxChar {
				e.current = r
				e.modified = false
				e.typingMode = false
				e.setStatus(fmt.Sprintf("Editing '%c'", r), 1)
			}
			return
		}
		// Normal preview text editing
		if len(e.previewText) < PreviewLimit {
			e.previewText += string(r)
		}
	}
}

func (e *Editor) moveCursor(dx, dy int) {
	e.cursorX += dx
	e.cursorY += dy

	if e.cursorX < 0 {
		e.cursorX = 0
	}
	if e.cursorX >= GridCols {
		e.cursorX = GridCols - 1
	}
	if e.cursorY < 0 {
		e.cursorY = 0
	}
	if e.cursorY >= GridRows {
		e.cursorY = GridRows - 1
	}
}

func (e *Editor) shiftLeft() {
	g := e.glyphs[e.current]
	for r := 0; r < GridRows; r++ {
		g[r] = g[r] << 1
	}
	e.glyphs[e.current] = g
}

func (e *Editor) shiftRight() {
	g := e.glyphs[e.current]
	for r := 0; r < GridRows; r++ {
		g[r] = g[r] >> 1
	}
	e.glyphs[e.current] = g
}

func (e *Editor) shiftUp() {
	g := e.glyphs[e.current]
	first := g[0]
	for r := 0; r < GridRows-1; r++ {
		g[r] = g[r+1]
	}
	g[GridRows-1] = first
	e.glyphs[e.current] = g
}

func (e *Editor) shiftDown() {
	g := e.glyphs[e.current]
	last := g[GridRows-1]
	for r := GridRows - 1; r > 0; r-- {
		g[r] = g[r-1]
	}
	g[0] = last
	e.glyphs[e.current] = g
}

func (e *Editor) flipHorizontal() {
	g := e.glyphs[e.current]
	for r := 0; r < GridRows; r++ {
		var newVal uint16
		for c := 0; c < GridCols; c++ {
			if (g[r] & (1 << (15 - c))) != 0 {
				// Write to mirrored MSB-aligned position
				newVal |= 1 << (15 - (GridCols - 1 - c))
			}
		}
		g[r] = newVal
	}
	e.glyphs[e.current] = g
}

func (e *Editor) flipVertical() {
	g := e.glyphs[e.current]
	for r := 0; r < GridRows/2; r++ {
		g[r], g[GridRows-1-r] = g[GridRows-1-r], g[r]
	}
	e.glyphs[e.current] = g
}

func (e *Editor) getBit(row, col int) bool {
	g := e.glyphs[e.current]
	mask := uint16(1) << (15 - col)
	return (g[row] & mask) != 0
}

func (e *Editor) setBit(row, col int, val bool) {
	g := e.glyphs[e.current]
	mask := uint16(1) << (15 - col)
	if val {
		g[row] |= mask
	} else {
		g[row] &^= mask
	}
	e.glyphs[e.current] = g
}

func (e *Editor) toggleBit(row, col int) {
	e.setBit(row, col, !e.getBit(row, col))
}

func (e *Editor) setStatus(msg string, msgType int) {
	e.statusMsg = msg
	e.statusType = msgType
	e.statusTimer = time.Now().Add(3 * time.Second)
}

func (e *Editor) copyToClipboard() {
	code := e.generateGoCode()

	// Try wl-copy (Wayland)
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err == nil {
		e.setStatus("Copied to clipboard (wl-copy)", 1)
		return
	}

	// Try xclip (X11)
	cmd = exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err == nil {
		e.setStatus("Copied to clipboard (xclip)", 1)
		return
	}

	// Try pbcopy (macOS)
	cmd = exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err == nil {
		e.setStatus("Copied to clipboard (pbcopy)", 1)
		return
	}

	e.setStatus("Clipboard copy failed - no clipboard tool found", 2)
}

func (e *Editor) exportAllGlyphs() {
	var buf bytes.Buffer
	buf.WriteString("var SplashFont = [95][12]uint16{\n")

	for i := 0; i < 95; i++ {
		r := rune(MinChar + i)
		g := e.glyphs[r]

		fmt.Fprintf(&buf, "\t// 0x%02X '%c'\n", r, r)
		fmt.Fprintln(&buf, "\t{")
		for row := 0; row < GridRows; row += 4 {
			fmt.Fprint(&buf, "\t\t")
			for j := 0; j < 4 && row+j < GridRows; j++ {
				if j > 0 {
					fmt.Fprint(&buf, " ")
				}
				fmt.Fprintf(&buf, "0x%04X,", g[row+j])
			}
			fmt.Fprintln(&buf)
		}
		fmt.Fprintln(&buf, "\t},")
	}
	buf.WriteString("}\n")

	code := buf.String()

	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err == nil {
		e.setStatus("Exported all glyphs to clipboard", 1)
		return
	}

	cmd = exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = strings.NewReader(code)
	if err := cmd.Run(); err == nil {
		e.setStatus("Exported all glyphs to clipboard", 1)
		return
	}

	e.setStatus("Export failed - no clipboard tool", 2)
}

func (e *Editor) generateGoCode() string {
	g := e.glyphs[e.current]
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "// 0x%02X '%c'\n", e.current, e.current)
	fmt.Fprintln(&buf, "{")
	for r := 0; r < GridRows; r += 4 {
		fmt.Fprint(&buf, "\t")
		for i := 0; i < 4 && r+i < GridRows; i++ {
			if i > 0 {
				fmt.Fprint(&buf, " ")
			}
			fmt.Fprintf(&buf, "0x%04X,", g[r+i])
		}
		fmt.Fprintln(&buf)
	}
	fmt.Fprint(&buf, "},")
	return buf.String()
}

// Rendering

func (e *Editor) draw() {
	cells := make([]terminal.Cell, e.width*e.height)

	bgCell := terminal.Cell{Rune: ' ', Bg: ColorBg}
	for i := range cells {
		cells[i] = bgCell
	}

	e.drawHeader(cells)
	e.drawGrid(cells)
	e.drawPreview(cells)
	e.drawCode(cells)
	e.drawCharSelector(cells)
	e.drawHelp(cells)
	e.drawStatus(cells)

	e.term.Flush(cells, e.width, e.height)
}

func (e *Editor) setCell(cells []terminal.Cell, x, y int, c terminal.Cell) {
	if x < 0 || x >= e.width || y < 0 || y >= e.height {
		return
	}
	cells[y*e.width+x] = c
}

func (e *Editor) drawText(cells []terminal.Cell, x, y int, text string, fg, bg terminal.RGB, attrs terminal.Attr) {
	for i, r := range text {
		e.setCell(cells, x+i, y, terminal.Cell{
			Rune:  r,
			Fg:    fg,
			Bg:    bg,
			Attrs: attrs,
		})
	}
}

func (e *Editor) drawBox(cells []terminal.Cell, x, y, w, h int, title string) {
	borderFg := ColorBorder

	e.setCell(cells, x, y, terminal.Cell{Rune: BoxTopLeft, Fg: borderFg, Bg: ColorBg})
	e.setCell(cells, x+w-1, y, terminal.Cell{Rune: BoxTopRight, Fg: borderFg, Bg: ColorBg})
	e.setCell(cells, x, y+h-1, terminal.Cell{Rune: BoxBottomLeft, Fg: borderFg, Bg: ColorBg})
	e.setCell(cells, x+w-1, y+h-1, terminal.Cell{Rune: BoxBottomRight, Fg: borderFg, Bg: ColorBg})

	for i := 1; i < w-1; i++ {
		e.setCell(cells, x+i, y, terminal.Cell{Rune: BoxHorizontal, Fg: borderFg, Bg: ColorBg})
		e.setCell(cells, x+i, y+h-1, terminal.Cell{Rune: BoxHorizontal, Fg: borderFg, Bg: ColorBg})
	}

	for i := 1; i < h-1; i++ {
		e.setCell(cells, x, y+i, terminal.Cell{Rune: BoxVertical, Fg: borderFg, Bg: ColorBg})
		e.setCell(cells, x+w-1, y+i, terminal.Cell{Rune: BoxVertical, Fg: borderFg, Bg: ColorBg})
	}

	if title != "" {
		e.drawText(cells, x+2, y, " "+title+" ", ColorHighlight, ColorBg, terminal.AttrBold)
	}
}

func (e *Editor) drawHeader(cells []terminal.Cell) {
	modMark := " "
	if e.modified {
		modMark = "*"
	}
	header := fmt.Sprintf(" VI-FIGHTER FONT EDITOR │ '%c' (0x%02X)%s ", e.current, e.current, modMark)
	startX := (e.width - len(header)) / 2
	if startX < 0 {
		startX = 0
	}
	e.drawText(cells, startX, 1, header, ColorText, ColorBg, terminal.AttrBold)
}

func (e *Editor) drawGrid(cells []terminal.Cell) {
	startX := 2
	startY := 3

	boxW := (GridCols * 2) + 4
	boxH := GridRows + 4

	e.drawBox(cells, startX, startY, boxW, boxH, "Glyph")

	// Column indicators
	for c := 0; c < GridCols; c++ {
		val := fmt.Sprintf("%X", c)
		color := ColorDim
		if c == e.cursorX {
			color = ColorHighlight
		}
		e.drawText(cells, startX+2+(c*2), startY+1, val, color, ColorBg, 0)
	}

	// Row indicators and grid
	for r := 0; r < GridRows; r++ {
		rowNum := fmt.Sprintf("%X", r)
		color := ColorDim
		if r == e.cursorY {
			color = ColorHighlight
		}
		e.drawText(cells, startX+1, startY+2+r, rowNum, color, ColorBg, 0)

		for c := 0; c < GridCols; c++ {
			active := e.getBit(r, c)
			isCursor := r == e.cursorY && c == e.cursorX

			screenX := startX + 2 + (c * 2)
			screenY := startY + 2 + r

			var cell terminal.Cell
			if active {
				cell = terminal.Cell{Rune: ' ', Bg: ColorPixelOn}
			} else {
				cell = terminal.Cell{Rune: DotMiddle, Fg: ColorDim, Bg: ColorGridBg}
			}

			if isCursor {
				if active {
					cell.Bg = ColorCursor
					cell.Rune = ' '
				} else {
					cell.Bg = ColorCursor
					cell.Fg = ColorBg
					cell.Rune = DotMiddle
				}
				cell.Attrs = terminal.AttrBlink
			}

			e.setCell(cells, screenX, screenY, cell)
			e.setCell(cells, screenX+1, screenY, cell)
		}
	}

	// Hex values on right side
	hexX := startX + boxW + 1
	for r := 0; r < GridRows; r++ {
		hexVal := fmt.Sprintf("0x%04X", e.glyphs[e.current][r])
		e.drawText(cells, hexX, startY+2+r, hexVal, ColorDim, ColorBg, 0)
	}
}

func (e *Editor) drawPreview(cells []terminal.Cell) {
	startX := 50
	startY := 3
	boxW := e.width - startX - 2
	if boxW < 20 {
		return
	}

	// Expanded height for 2-3 rows of preview glyphs
	// Each glyph row = 6 screen rows (12 glyph rows / 2 with half-blocks)
	// 2 rows of glyphs = 12 screen rows + 2 for text + 2 for borders = 16
	boxH := 16

	title := "Preview"
	if e.typingMode {
		title = "Preview [TYPING]"
	}
	e.drawBox(cells, startX, startY, boxW, boxH, title)

	// Preview text display
	previewY := startY + 1
	maxTextW := boxW - 4
	displayText := e.previewText
	if len(displayText) > maxTextW {
		displayText = displayText[:maxTextW]
	}
	e.drawText(cells, startX+2, previewY, displayText, ColorDim, ColorBg, 0)

	// Calculate how many chars fit per row
	pAreaX := startX + 2
	pAreaW := boxW - 4
	charsPerRow := pAreaW / (GridCols + 1)
	if charsPerRow < 1 {
		charsPerRow = 1
	}

	// Render preview glyphs in wrapped rows
	charIdx := 0
	glyphRowStart := startY + 2

	for rowNum := 0; rowNum < 2 && charIdx < len(e.previewText); rowNum++ {
		renderX := pAreaX
		pAreaY := glyphRowStart + (rowNum * 7) // 6 screen rows per glyph row + 1 spacing

		for col := 0; col < charsPerRow && charIdx < len(e.previewText); col++ {
			r := rune(e.previewText[charIdx])
			charIdx++

			glyph, ok := e.glyphs[r]
			if !ok {
				renderX += GridCols + 1
				continue
			}

			// Draw using half-block characters (2 glyph rows per screen row)
			for y := 0; y < GridRows && y/2 < 6; y += 2 {
				screenY := pAreaY + (y / 2)
				if screenY >= startY+boxH-1 {
					break
				}

				for x := 0; x < GridCols; x++ {
					if renderX+x >= pAreaX+pAreaW {
						break
					}

					mask := uint16(1) << (15 - x)
					top := (glyph[y] & mask) != 0
					bot := y+1 < GridRows && (glyph[y+1]&mask) != 0

					fg := ColorPixelOn
					if r == e.current {
						fg = ColorHighlight
					}

					cell := terminal.Cell{Bg: ColorBg, Fg: fg}
					switch {
					case top && bot:
						cell.Rune = BlockFull
					case top:
						cell.Rune = BlockUpper
					case bot:
						cell.Rune = BlockLower
					default:
						cell.Rune = ' '
					}

					e.setCell(cells, renderX+x, screenY, cell)
				}
			}
			renderX += GridCols + 1
		}
	}
}

func (e *Editor) drawCode(cells []terminal.Cell) {
	startX := 50
	startY := 20 // Moved down to accommodate larger preview
	boxW := e.width - startX - 2
	if boxW < 20 {
		return
	}
	boxH := 9 // Increased to show all 7 lines of code

	e.drawBox(cells, startX, startY, boxW, boxH, "Go Code [y=copy]")

	code := e.generateGoCode()
	lines := strings.Split(code, "\n")

	for i, line := range lines {
		if i >= boxH-2 {
			break
		}
		fg := ColorText
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			fg = ColorDim
		} else if strings.Contains(line, "0x") {
			fg = terminal.RGB{R: 150, G: 200, B: 255}
		}
		displayLine := strings.ReplaceAll(line, "\t", "  ")
		if len(displayLine) > boxW-4 {
			displayLine = displayLine[:boxW-4]
		}
		e.drawText(cells, startX+2, startY+1+i, displayLine, fg, ColorBg, 0)
	}
}

func (e *Editor) drawCharSelector(cells []terminal.Cell) {
	startX := 2
	startY := 20
	boxW := 46
	boxH := e.height - startY - 5
	if boxH < 4 {
		return
	}

	e.drawBox(cells, startX, startY, boxW, boxH, "Chars [/]=jump")

	// Show character grid
	charsPerRow := (boxW - 4) / 2
	y := startY + 1
	x := startX + 2

	for c := MinChar; c <= MaxChar; c++ {
		r := rune(c)
		fg := ColorDim
		bg := ColorBg
		if r == e.current {
			fg = ColorBg
			bg = ColorHighlight
		}

		e.setCell(cells, x, y, terminal.Cell{Rune: r, Fg: fg, Bg: bg})

		x += 2
		if (c-MinChar+1)%charsPerRow == 0 {
			x = startX + 2
			y++
			if y >= startY+boxH-1 {
				break
			}
		}
	}
}

func (e *Editor) drawHelp(cells []terminal.Cell) {
	y := e.height - 5
	if y < 0 {
		return
	}

	help := []string{
		"Move: WASD/HJKL/Arrows  │  Toggle: SPACE  │  Set: o/ENTER  │  Clear: x/DEL  │  Char: [/]",
		"Shift: <>/^v  │  Flip: |/_  │  Clear: c  │  Invert: i  │  Reset: r  │  Glyph: Y=copy p=paste",
		"Row: X=clear F=fill R=yank P=paste O=ins↑ N=ins↓ Z=del  │  Preview: t  │  Jump: /",
		"Export: y (char) E (all)  │  Quit: q/ESC",
	}

	for i, h := range help {
		if y+i >= e.height {
			break
		}
		e.drawText(cells, 2, y+i, h, ColorDim, ColorBg, 0)
	}
}

func (e *Editor) drawStatus(cells []terminal.Cell) {
	if e.statusMsg == "" {
		return
	}

	barY := e.height - 1
	msg := " " + e.statusMsg + " "

	bg := terminal.RGB{R: 60, G: 60, B: 80}
	switch e.statusType {
	case 1:
		bg = ColorSuccess
	case 2:
		bg = ColorError
	}

	x := e.width - len(msg) - 2
	if x < 0 {
		x = 0
	}
	e.drawText(cells, x, barY, msg, terminal.RGB{R: 255, G: 255, B: 255}, bg, terminal.AttrBold)
}