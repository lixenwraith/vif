// @lixen: #dev{feature[lightning(render)],feature[shield(render,system)],feature[spirit(render,system)]}
package render

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Cell is an alias to terminal.Cell to avoid copying
// Attributes are preserved directly
type Cell = terminal.Cell
type Attr = terminal.Attr

// DefaultBgRGB is the default background color (Tokyo Night)
var DefaultBgRGB = RgbBackground