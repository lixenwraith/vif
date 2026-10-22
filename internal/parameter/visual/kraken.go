package visual

import "github.com/lixenwraith/color"

const (
	KrakenHealthBarWidth = 9

	// A tentacle's rim reaches KrakenRimWidth cells past its disc, a charge heats a
	// leg's tip KrakenMoveHeat of the way to the attack color, and the body's void
	// clears the glyphs under it where it is more than KrakenVoidOpaque opaque
	KrakenRimWidth   = 1.8
	KrakenMoveHeat   = 0.4
	KrakenVoidOpaque = 0.3
)

// KrakenRimShades grade the rim by how far past the disc a cell lies, as a fraction
// of KrakenRimWidth; a cell past the last stays clear
var KrakenRimShades = [...]struct {
	Below float64
	Rune  rune
}{{0.25, '▓'}, {0.55, '▒'}, {0.85, '░'}}

var (
	RgbKrakenVoid    = color.RGB{R: 12, G: 12, B: 18}
	RgbKrakenLegBase = color.RGB{R: 35, G: 10, B: 50}
	RgbKrakenLegTip  = color.RGB{R: 80, G: 15, B: 90}
	RgbKrakenAttack  = color.RGB{R: 255, G: 30, B: 60}
)
