package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
)

// SquareKindColors are what a magic square's blocks of one kind draw in: a cell
// whole and worn to nothing, the glow its effect holds in, and a console's color
type SquareKindColors struct {
	Whole, Worn color.RGB
	Glow        color.RGB
	Console     uint8
}

// SquareKinds holds each kind's colors
var SquareKinds = [component.SquareKindCount]SquareKindColors{
	component.SquareAttack:  {Whole: color.Vermilion, Worn: color.DarkCrimson, Glow: color.Coral, Console: ConRed},
	component.SquareHold:    {Whole: color.Gold, Worn: color.DarkGold, Glow: color.Apricot, Console: ConYellow},
	component.SquareDefense: {Whole: color.BrightCyan, Worn: color.Teal, Glow: color.SkyTeal, Console: ConCyan},
}

// The square's marks: a standing block's digit, the frame round the viewer's
// selection, a fallen block's outline and a recharging kind in the legend under it
var (
	RgbSquareDigit  = color.Black
	RgbSquareFrame  = color.White
	RgbSquareFallen = color.DarkGray
)

const (
	// SquareFallenChar dots a fallen block's edge
	SquareFallenChar = '·'

	// SquareWornChar256 shades a cell a console shows worn: one below SquareWorn256
	// of its hit points
	SquareWornChar256 = '▒'
	SquareWorn256     = 0.5

	// SquareHitFlash is how far a cell a hit just landed on goes toward white
	SquareHitFlash = 0.5

	// SquareLegendGap is the cells between two kinds in the legend
	SquareLegendGap = 3

	// The shield's glow pulses between these intensities each period, fading over
	// SquareShieldGlowBand of its radius inward from its rim
	SquareShieldGlowMin      = 0.15
	SquareShieldGlowMax      = 0.45
	SquareShieldGlowPeriodMs = 900
	SquareShieldGlowBand     = 0.35

	// SquareShieldRim256 is the share of the band's glow a console shows as the rim
	SquareShieldRim256 = 0.6
)
