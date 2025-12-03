package render

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Cell is an alias to terminal.Cell to avoid copying.
// Attributes are preserved directly.
type Cell = terminal.Cell
type Attr = terminal.Attr

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = RGB{26, 27, 38}

var emptyCell = Cell{
	Rune:  ' ',
	Fg:    DefaultBgRGB,
	Bg:    DefaultBgRGB,
	Attrs: terminal.AttrNone,
}

// EmptyCell returns a copy of the empty cell sentinel
func EmptyCell() Cell {
	return emptyCell
}