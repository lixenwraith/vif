package render

import (
	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/core"
)

// CompositorCell is the authoritative cell state
// Stores RGB colors directly, attributes preserved as tcell.AttrMask
type CompositorCell struct {
	Rune  rune
	Fg    core.RGB
	Bg    core.RGB
	Attrs tcell.AttrMask // Preserved exactly - includes bit 31 (AttrInvalid)
}

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = core.RGB{26, 27, 38}

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