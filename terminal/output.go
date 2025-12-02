package terminal

import (
	"bufio"
	"io"
)

// outputBuffer manages double-buffered terminal output with diffing
type outputBuffer struct {
	front     []Cell // What's currently on screen
	width     int
	height    int
	colorMode ColorMode
	writer    *bufio.Writer

	// State tracking for optimization
	cursorX   int
	cursorY   int
	lastFg    RGB
	lastBg    RGB
	lastAttr  Attr
	lastValid bool // Whether last* values are valid

	// Tracks if cursor position is known to match valid X/Y
	cursorValid bool
}

// newOutputBuffer creates a new output buffer
func newOutputBuffer(w io.Writer, colorMode ColorMode) *outputBuffer {
	return &outputBuffer{
		writer:    bufio.NewWriterSize(w, 65536), // 64KB buffer
		colorMode: colorMode,
	}
}

// resize updates buffer dimensions
func (o *outputBuffer) resize(width, height int) {
	size := width * height
	if cap(o.front) < size {
		o.front = make([]Cell, size)
	} else {
		o.front = o.front[:size]
	}
	o.width = width
	o.height = height

	// Clear front buffer to force full redraw
	for i := range o.front {
		o.front[i] = Cell{Rune: 0} // Invalid rune forces redraw
	}
	o.lastValid = false
}

// flush writes the back buffer to terminal, diffing against front buffer
func (o *outputBuffer) flush(cells []Cell, width, height int) {
	if width != o.width || height != o.height {
		o.resize(width, height)
	}

	expectedSize := width * height
	if len(cells) < expectedSize {
		return
	}

	w := o.writer

	// We track cursor persistence
	// If cursor state is invalid (e.g. after clear), we simply ensure the first write generates a move

	for y := 0; y < height; y++ {
		rowStart := y * width
		x := 0

		for x < width {
			idx := rowStart + x
			newCell := cells[idx]
			oldCell := o.front[idx]

			// Check if cell changed
			if o.cellEqual(newCell, oldCell) {
				x++
				continue
			}

			// Find run of changed cells
			runEnd := x + 1
			for runEnd < width {
				runIdx := rowStart + runEnd
				if o.cellEqual(cells[runIdx], o.front[runIdx]) {
					break
				}
				nextCell := cells[runIdx]
				if !newCell.Fg.Equal(nextCell.Fg) ||
					!newCell.Bg.Equal(nextCell.Bg) ||
					newCell.Attrs != nextCell.Attrs {
					break
				}
				runEnd++
			}

			// Move cursor if needed
			if !o.cursorValid || x != o.cursorX || y != o.cursorY {
				// Optimization: Check for horizontal jump on same line
				if o.cursorValid && y == o.cursorY && x > o.cursorX {
					gap := x - o.cursorX
					if gap < 4 {
						// Small gap: write spaces (1-3 bytes)
						for i := 0; i < gap; i++ {
							w.WriteByte(' ')
						}
					} else {
						// Large gap: use relative move forward (smaller sequence than absolute move)
						writeCursorForward(w, gap)
					}
					o.cursorX = x
				} else {
					// Different line or backward jump: absolute position
					writeCursorPos(w, x, y)
					o.cursorX = x
					o.cursorY = y
					o.cursorValid = true
				}
			}

			// Write style if changed
			o.writeStyle(w, newCell.Fg, newCell.Bg, newCell.Attrs)

			// Write the run
			for i := x; i < runEnd; i++ {
				c := cells[rowStart+i]
				r := c.Rune
				if r == 0 {
					r = ' '
				}
				writeRune(w, r)
				o.front[rowStart+i] = c
			}

			o.cursorX = runEnd
			x = runEnd
		}
	}

	// Reset attributes at end of frame to be safe, but keep cursor position valid
	w.Write(csiSGR0)
	o.lastValid = false // Invalidate style cache

	w.Flush()
}

// forceFullRedraw clears front buffer to force complete redraw
func (o *outputBuffer) forceFullRedraw() {
	for i := range o.front {
		o.front[i] = Cell{Rune: 0}
	}
	o.lastValid = false
}

// cellEqual compares two cells for equality
func (o *outputBuffer) cellEqual(a, b Cell) bool {
	if a.Rune != b.Rune {
		return false
	}
	if a.Rune == 0 && b.Rune == 0 {
		// Both empty, compare backgrounds only
		return a.Bg.Equal(b.Bg)
	}
	return a.Fg.Equal(b.Fg) && a.Bg.Equal(b.Bg) && a.Attrs == b.Attrs
}

// writeStyle writes color and attribute codes if changed
func (o *outputBuffer) writeStyle(w *bufio.Writer, fg, bg RGB, attr Attr) {
	fgChanged := !o.lastValid || !fg.Equal(o.lastFg)
	bgChanged := !o.lastValid || !bg.Equal(o.lastBg)
	attrChanged := !o.lastValid || attr != o.lastAttr

	if !fgChanged && !bgChanged && !attrChanged {
		return
	}

	// If attributes changed, reset and reapply everything
	if attrChanged {
		w.Write(csiSGR0)
		writeAttrs(w, attr)
		writeFgColor(w, fg, o.colorMode)
		writeBgColor(w, bg, o.colorMode)
	} else {
		if fgChanged {
			writeFgColor(w, fg, o.colorMode)
		}
		if bgChanged {
			writeBgColor(w, bg, o.colorMode)
		}
	}

	o.lastFg = fg
	o.lastBg = bg
	o.lastAttr = attr
	o.lastValid = true
}

// clear writes a clear screen with specified background
func (o *outputBuffer) clear(bg RGB) {
	w := o.writer
	w.Write(csiSGR0)
	writeBgColor(w, bg, o.colorMode)
	w.Write(csiClear)

	o.lastValid = false
	o.cursorValid = false // Cursor position lost after clear
	w.Flush()

	// Update front buffer to match
	for i := range o.front {
		o.front[i] = Cell{Rune: ' ', Bg: bg}
	}
}

// invalidateCursor marks the cursor position as unknown
// Call this if external writes move the cursor
func (o *outputBuffer) invalidateCursor() {
	o.cursorValid = false
}