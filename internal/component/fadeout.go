package component

import "github.com/lixenwraith/color"

// FadeoutComponent represents a visual fade-out effect for background cells; its
// lifetime timer times the fade
type FadeoutComponent struct {
	Char    rune // 0 = bg-only fadeout
	FgColor color.RGB
	BgColor color.RGB
}
