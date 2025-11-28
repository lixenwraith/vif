package render

import "github.com/gdamore/tcell/v2"

// BufferScreen implements tcell.Screen-like interface writing to RenderBuffer.
// Migration shim allowing legacy draw functions to work unchanged.
type BufferScreen struct {
	buf *RenderBuffer
}

// NewBufferScreen wraps a RenderBuffer for screen-like access.
func NewBufferScreen(buf *RenderBuffer) *BufferScreen {
	return &BufferScreen{buf: buf}
}

// SetContent writes to the buffer. Combining runes are ignored.
func (bs *BufferScreen) SetContent(x, y int, primary rune, combining []rune, style tcell.Style) {
	bs.buf.Set(x, y, primary, style)
}

// GetContent reads from the buffer. Width is always 1, combining always nil.
func (bs *BufferScreen) GetContent(x, y int) (primary rune, combining []rune, style tcell.Style, width int) {
	cell := bs.buf.Get(x, y)
	return cell.Rune, nil, cell.Style, 1
}

// Size returns buffer dimensions.
func (bs *BufferScreen) Size() (int, int) {
	return bs.buf.Bounds()
}