package component

import (
	"time"

	"github.com/lixenwraith/color"
)

// FadeoutComponent represents a visual fade-out effect for background cells
type FadeoutComponent struct {
	Char      rune // 0 = bg-only fadeout
	FgColor   color.RGB
	BgColor   color.RGB
	Remaining time.Duration
	Duration  time.Duration
}

