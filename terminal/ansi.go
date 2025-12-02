package terminal

import (
	"bufio"
	"strconv"
)

// Pre-allocated ANSI sequence fragments (avoid allocations during render)
var (
	// CSI sequences
	csi      = []byte("\x1b[")
	csiEnd   = []byte("m")
	csiReset = []byte("\x1b[0m")
	csiClear = []byte("\x1b[2J\x1b[H")
	csiHome  = []byte("\x1b[H")
	csiRIS   = []byte("\x1bc") // Reset to Initial State (emergency)
	csiSGR0  = []byte("\x1b[0m")

	// Cursor control
	csiCursorHide = []byte("\x1b[?25l")
	csiCursorShow = []byte("\x1b[?25h")
	csiCursorPos  = []byte("\x1b[") // followed by row;colH

	// Alternate screen buffer
	csiAltScreenEnter = []byte("\x1b[?1049h")
	csiAltScreenExit  = []byte("\x1b[?1049l")

	// Color prefixes
	csiFg256     = []byte("\x1b[38;5;") // followed by N;m
	csiBg256     = []byte("\x1b[48;5;") // followed by N;m
	csiFgRGB     = []byte("\x1b[38;2;") // followed by R;G;B;m
	csiBgRGB     = []byte("\x1b[48;2;") // followed by R;G;B;m
	csiDefaultFg = []byte("\x1b[39m")
	csiDefaultBg = []byte("\x1b[49m")

	// Attribute sequences
	csiAttrBold      = []byte("\x1b[1m")
	csiAttrDim       = []byte("\x1b[2m")
	csiAttrItalic    = []byte("\x1b[3m")
	csiAttrUnderline = []byte("\x1b[4m")
	csiAttrBlink     = []byte("\x1b[5m")
	csiAttrReverse   = []byte("\x1b[7m")
)

// writeInt writes an integer to the buffer without allocation
func writeInt(w *bufio.Writer, n int) {
	if n < 0 {
		n = 0
	}
	if n < 10 {
		w.WriteByte('0' + byte(n))
		return
	}
	if n < 100 {
		w.WriteByte('0' + byte(n/10))
		w.WriteByte('0' + byte(n%10))
		return
	}
	if n < 1000 {
		w.WriteByte('0' + byte(n/100))
		w.WriteByte('0' + byte((n/10)%10))
		w.WriteByte('0' + byte(n%10))
		return
	}

	var buf [20]byte
	b := strconv.AppendInt(buf[:0], int64(n), 10)
	w.Write(b)
}

// writeFgColor writes foreground color sequence
func writeFgColor(w *bufio.Writer, c RGB, mode ColorMode) {
	if mode == ColorModeTrueColor {
		w.Write(csiFgRGB)
		writeInt(w, int(c.R))
		w.WriteByte(';')
		writeInt(w, int(c.G))
		w.WriteByte(';')
		writeInt(w, int(c.B))
		w.WriteByte('m')
	} else {
		w.Write(csiFg256)
		writeInt(w, int(RGBTo256(c)))
		w.WriteByte('m')
	}
}

// writeBgColor writes background color sequence
func writeBgColor(w *bufio.Writer, c RGB, mode ColorMode) {
	if mode == ColorModeTrueColor {
		w.Write(csiBgRGB)
		writeInt(w, int(c.R))
		w.WriteByte(';')
		writeInt(w, int(c.G))
		w.WriteByte(';')
		writeInt(w, int(c.B))
		w.WriteByte('m')
	} else {
		w.Write(csiBg256)
		writeInt(w, int(RGBTo256(c)))
		w.WriteByte('m')
	}
}

// writeAttrs writes attribute sequences for changed attributes
func writeAttrs(w *bufio.Writer, attrs Attr) {
	if attrs == AttrNone {
		return
	}
	if attrs&AttrBold != 0 {
		w.Write(csiAttrBold)
	}
	if attrs&AttrDim != 0 {
		w.Write(csiAttrDim)
	}
	if attrs&AttrItalic != 0 {
		w.Write(csiAttrItalic)
	}
	if attrs&AttrUnderline != 0 {
		w.Write(csiAttrUnderline)
	}
	if attrs&AttrBlink != 0 {
		w.Write(csiAttrBlink)
	}
	if attrs&AttrReverse != 0 {
		w.Write(csiAttrReverse)
	}
}

// writeCursorPos writes cursor positioning sequence (0-indexed input, 1-indexed output)
func writeCursorPos(w *bufio.Writer, x, y int) {
	w.Write(csiCursorPos)
	writeInt(w, y+1)
	w.WriteByte(';')
	writeInt(w, x+1)
	w.WriteByte('H')
}

// writeCursorForward writes cursor forward N positions
func writeCursorForward(w *bufio.Writer, n int) {
	if n <= 0 {
		return
	}
	if n == 1 {
		w.Write([]byte("\x1b[C"))
		return
	}
	w.Write(csi)
	writeInt(w, n)
	w.WriteByte('C')
}

// writeRune writes a rune as UTF-8 bytes
func writeRune(w *bufio.Writer, r rune) {
	w.WriteRune(r)
}