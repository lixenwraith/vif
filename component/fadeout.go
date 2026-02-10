package component

import (
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// FadeoutComponent represents a visual fade-out effect for background cells
type FadeoutComponent struct {
	Char      rune // 0 = bg-only fadeout
	FgColor   terminal.RGB
	BgColor   terminal.RGB
	Remaining time.Duration
	Duration  time.Duration
}