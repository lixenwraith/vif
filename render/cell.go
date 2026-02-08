package render

import (
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Cell is an alias to terminal.Cell to avoid copying
// Attributes are preserved directly
type Cell = terminal.Cell
type Attr = terminal.Attr

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = visual.RgbBackground