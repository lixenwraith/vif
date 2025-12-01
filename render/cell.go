package render

import (
	"github.com/gdamore/tcell/v2"
)

// CompositorCell is the authoritative cell state
// Stores RGB colors directly, attributes preserved as tcell.AttrMask
type CompositorCell struct {
	Rune  rune
	Fg    RGB
	Bg    RGB
	Attrs tcell.AttrMask
}

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = RGB{26, 27, 38}

var emptyCell = CompositorCell{
	Rune:  ' ',
	Fg:    DefaultBgRGB,
	Bg:    DefaultBgRGB,
	Attrs: tcell.AttrNone,
}

// EmptyCell returns a copy of the empty cell sentinel
func EmptyCell() CompositorCell {
	return emptyCell
}