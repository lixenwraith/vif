package render

import "github.com/gdamore/tcell/v2"

// RenderCell represents terminal cell state.
type RenderCell struct {
	Rune  rune
	Style tcell.Style
}

var emptyCell = RenderCell{
	Rune:  ' ',
	Style: tcell.StyleDefault,
}

// EmptyCell returns a copy of the empty cell sentinel.
func EmptyCell() RenderCell {
	return emptyCell
}