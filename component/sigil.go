package component

import (
	"github.com/lixenwraith/color"
)

// SigilComponent provides visual representation for non-typeable moving entities
type SigilComponent struct {
	Rune  rune
	Color color.RGB
}

