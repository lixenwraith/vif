package component

import (
	"github.com/lixenwraith/vi-fighter/terminal"
)

// SigilComponent provides visual representation for non-typeable moving entities
type SigilComponent struct {
	Rune  rune
	Color terminal.RGB
}