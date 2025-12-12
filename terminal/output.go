// @lixen: #focus{sys[term,io,output]}
// @lixen: #interact{trigger[output,ansi]}
package terminal

import (
	"bufio"
	"io"
)

// outputBuffer manages double-buffered terminal output with diffing
type outputBuffer struct {
	front     []Cell
	width     int
	height    int
	colorMode ColorMode
	writer    *bufio.Writer

	cursorX     int
	cursorY     int
	cursorValid bool

	// Style state for coalescing
	lastFg    RGB
	lastBg    RGB
	lastAttr  Attr
	lastValid bool
}

// newOutputBuffer creates a new output buffer
func newOutputBuffer(w io.Writer, colorMode ColorMode) *outputBuffer {
	return &outputBuffer{
		writer:    bufio.NewWriterSize(w, 131072), // 128KB buffer
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

	for i := range o.front {
		o.front[i] = Cell{Rune: 0}
	}
	o.lastValid = false
	o.cursorValid = false
}

// cellEqual compares two cells for equality (standalone for inlining)
// Optimization: Skip foreground check to save CPU cycles considering current game context
// TODO: no issue found in tests, trace to make sure doesn't break space
func cellEqual(a, b Cell) bool {
	if a.Rune != b.Rune || a.Attrs != b.Attrs {
		return false
	}
	if a.Rune == 0 {
		return a.Bg == b.Bg
	}
	return a.Fg == b.Fg && a.Bg == b.Bg
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

	for y := 0; y < height; y++ {
		rowStart := y * width
		x := 0

		for x < width {
			idx := rowStart + x
			newCell := cells[idx]

			if cellEqual(newCell, o.front[idx]) {
				x++
				continue
			}

			// Position cursor once for this dirty region
			if !o.cursorValid || x != o.cursorX || y != o.cursorY {
				// Always use non-destructive cursor movement
				// TODO: review, optimization attempt for multiple jumps with ' ' write is destructive
				if o.cursorValid && y == o.cursorY && x > o.cursorX {
					writeCursorForward(w, x-o.cursorX)
				} else {
					writeCursorPos(w, x, y)
				}
				o.cursorX = x
				o.cursorY = y
				o.cursorValid = true
			}

			// Write all contiguous dirty cells, emitting style only when changed
			for x < width {
				cidx := rowStart + x
				c := cells[cidx]

				if cellEqual(c, o.front[cidx]) {
					break
				}

				o.writeStyleCoalesced(w, c.Fg, c.Bg, c.Attrs)

				r := c.Rune
				if r == 0 {
					r = ' '
				}
				if r < 0x80 {
					w.WriteByte(byte(r))
				} else {
					w.WriteRune(r)
				}

				o.front[cidx] = c
				o.cursorX++
				x++
			}
		}
	}

	w.Write(csiSGR0)
	o.lastValid = false

	w.Flush()
}

// writeStyleCoalesced emits a single combined SGR sequence when style changes
func (o *outputBuffer) writeStyleCoalesced(w *bufio.Writer, fg, bg RGB, attr Attr) {
	// Check what changed
	fgChanged := !o.lastValid || fg != o.lastFg || (attr&AttrFg256) != (o.lastAttr&AttrFg256)
	bgChanged := !o.lastValid || bg != o.lastBg || (attr&AttrBg256) != (o.lastAttr&AttrBg256)
	styleAttr := attr & AttrStyle
	lastStyleAttr := o.lastAttr & AttrStyle
	attrChanged := !o.lastValid || styleAttr != lastStyleAttr

	if !fgChanged && !bgChanged && !attrChanged {
		return
	}

	// If attributes changed, must reset first
	if attrChanged {
		w.Write(csi) // \x1b[
		first := true

		// Reset
		w.WriteByte('0')
		first = false

		// Style attributes
		if styleAttr&AttrBold != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('1')
			first = false
		}
		if styleAttr&AttrDim != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('2')
			first = false
		}
		if styleAttr&AttrItalic != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('3')
			first = false
		}
		if styleAttr&AttrUnderline != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('4')
			first = false
		}
		if styleAttr&AttrBlink != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('5')
			first = false
		}
		if styleAttr&AttrReverse != 0 {
			if !first {
				w.WriteByte(';')
			}
			w.WriteByte('7')
			first = false
		}

		// Foreground
		o.writeFgInline(w, fg, attr)

		// Background
		o.writeBgInline(w, bg, attr)

		w.WriteByte('m')
	} else {
		// Only colors changed, emit minimal sequence
		if fgChanged && bgChanged {
			w.Write(csi)
			o.writeFgInline(w, fg, attr)
			o.writeBgInline(w, bg, attr)
			w.WriteByte('m')
		} else if fgChanged {
			o.writeFgFull(w, fg, attr)
		} else if bgChanged {
			o.writeBgFull(w, bg, attr)
		}
	}

	o.lastFg = fg
	o.lastBg = bg
	o.lastAttr = attr
	o.lastValid = true
}

// writeFgInline writes fg color parameters (no CSI prefix, no 'm' suffix)
func (o *outputBuffer) writeFgInline(w *bufio.Writer, fg RGB, attr Attr) {
	w.WriteByte(';')
	if attr&AttrFg256 != 0 {
		// 256-color: 38;5;N
		w.Write([]byte("38;5;"))
		writeInt(w, int(fg.R))
	} else if o.colorMode == ColorModeTrueColor {
		// True color: 38;2;R;G;B
		w.Write([]byte("38;2;"))
		writeInt(w, int(fg.R))
		w.WriteByte(';')
		writeInt(w, int(fg.G))
		w.WriteByte(';')
		writeInt(w, int(fg.B))
	} else {
		// Fallback 256: 38;5;N
		w.Write([]byte("38;5;"))
		writeInt(w, int(RGBTo256(fg)))
	}
}

// writeBgInline writes bg color parameters (no CSI prefix, no 'm' suffix)
func (o *outputBuffer) writeBgInline(w *bufio.Writer, bg RGB, attr Attr) {
	w.WriteByte(';')
	if attr&AttrBg256 != 0 {
		// 256-color: 48;5;N
		w.Write([]byte("48;5;"))
		writeInt(w, int(bg.R))
	} else if o.colorMode == ColorModeTrueColor {
		// True color: 48;2;R;G;B
		w.Write([]byte("48;2;"))
		writeInt(w, int(bg.R))
		w.WriteByte(';')
		writeInt(w, int(bg.G))
		w.WriteByte(';')
		writeInt(w, int(bg.B))
	} else {
		// Fallback 256: 48;5;N
		w.Write([]byte("48;5;"))
		writeInt(w, int(RGBTo256(bg)))
	}
}

// writeFgFull writes complete fg color sequence
func (o *outputBuffer) writeFgFull(w *bufio.Writer, fg RGB, attr Attr) {
	if attr&AttrFg256 != 0 {
		w.Write(csiFg256)
		writeInt(w, int(fg.R))
		w.WriteByte('m')
	} else if o.colorMode == ColorModeTrueColor {
		w.Write(csiFgRGB)
		writeInt(w, int(fg.R))
		w.WriteByte(';')
		writeInt(w, int(fg.G))
		w.WriteByte(';')
		writeInt(w, int(fg.B))
		w.WriteByte('m')
	} else {
		w.Write(csiFg256)
		writeInt(w, int(RGBTo256(fg)))
		w.WriteByte('m')
	}
}

// writeBgFull writes complete bg color sequence
func (o *outputBuffer) writeBgFull(w *bufio.Writer, bg RGB, attr Attr) {
	if attr&AttrBg256 != 0 {
		w.Write(csiBg256)
		writeInt(w, int(bg.R))
		w.WriteByte('m')
	} else if o.colorMode == ColorModeTrueColor {
		w.Write(csiBgRGB)
		writeInt(w, int(bg.R))
		w.WriteByte(';')
		writeInt(w, int(bg.G))
		w.WriteByte(';')
		writeInt(w, int(bg.B))
		w.WriteByte('m')
	} else {
		w.Write(csiBg256)
		writeInt(w, int(RGBTo256(bg)))
		w.WriteByte('m')
	}
}

// forceFullRedraw clears front buffer to force complete redraw
func (o *outputBuffer) forceFullRedraw() {
	for i := range o.front {
		o.front[i] = Cell{Rune: 0}
	}
	o.lastValid = false
	o.cursorValid = false
}

// clear writes a clear screen with specified background
func (o *outputBuffer) clear(bg RGB) {
	w := o.writer
	w.Write(csiSGR0)
	o.writeBgFull(w, bg, 0)
	w.Write(csiClear)

	o.lastValid = false
	o.cursorValid = false
	w.Flush()

	for i := range o.front {
		o.front[i] = Cell{Rune: ' ', Bg: bg}
	}
}

// invalidateCursor marks cursor position as unknown
func (o *outputBuffer) invalidateCursor() {
	o.cursorValid = false
}