package render

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// CompositorCell is the authoritative cell state
// Stores RGB colors directly, attributes preserved as terminal.Attr
type CompositorCell struct {
	Rune  rune
	Fg    RGB
	Bg    RGB
	Attrs terminal.Attr
}

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = RGB{26, 27, 38}

var emptyCell = CompositorCell{
	Rune:  ' ',
	Fg:    DefaultBgRGB,
	Bg:    DefaultBgRGB,
	Attrs: terminal.AttrNone,
}

// EmptyCell returns a copy of the empty cell sentinel
func EmptyCell() CompositorCell {
	return emptyCell
}