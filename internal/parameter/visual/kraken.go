package visual

import "github.com/lixenwraith/color"

const (
	KrakenHealthBarWidth = 9

	// A tentacle's rim draws KrakenRimRune less than KrakenRimReach cells past its
	// disc, a charge heats a leg's tip KrakenMoveHeat of the way to the attack color,
	// and the body's void clears the glyphs under it where it is more than
	// KrakenVoidOpaque opaque
	KrakenRimReach   = 1.53
	KrakenRimRune    = '░'
	KrakenMoveHeat   = 0.4
	KrakenVoidOpaque = 0.3
)

// In 256 colors the console shows the legs' dark gradient and the body's void as
// black, so legs, rim and body draw in Kraken256Leg alike, and a leg heated past
// Effect256Threshold toward the attack color in Kraken256Heat
const (
	Kraken256Leg  = ConMagenta
	Kraken256Heat = ConBrightRed
)

var (
	RgbKrakenVoid    = color.RGB{R: 12, G: 12, B: 18}
	RgbKrakenLegBase = color.RGB{R: 35, G: 10, B: 50}
	RgbKrakenLegTip  = color.RGB{R: 80, G: 15, B: 90}
	RgbKrakenAttack  = color.RGB{R: 255, G: 30, B: 60}
)
