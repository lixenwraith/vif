package visual

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
)

// SquareKindColors are what a magic square's blocks of one kind draw in: a cell
// whole and worn to nothing, the glow its line fires and its effect holds in, and a
// console's color
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
// selection, a fallen block's outline and a kind no line holds in the legend under it
var (
	RgbSquareDigit  = color.Black
	RgbSquareFrame  = color.White
	RgbSquareFallen = color.DarkGray
)

const (
	// SquareFallenChar dots a fallen block's edge, and a console's empty meter cell
	SquareFallenChar = '·'

	// SquareWornChar256 shades a cell a console shows worn: one below SquareWorn256
	// of its hit points
	SquareWornChar256 = '▒'
	SquareWorn256     = 0.5

	// SquareHitFlash is how far a cell a hit just landed on goes toward white
	SquareHitFlash = 0.5

	// SquareFireFlash is how long a kind's blocks take to fade from its glow as its
	// line fires
	SquareFireFlash = 400 * time.Millisecond

	// The legend gives each kind its name and a meter of SquareMeterCells, its empty
	// cells SquareMeterTrack of the way from the background to the kind's worn color,
	// SquareLegendGap cells parting two kinds
	SquareMeterCells = 6
	SquareMeterTrack = 0.3
	SquareLegendGap  = 3

	// The shield's glow pulses between these intensities each period, fading over
	// SquareShieldGlowBand of its radius inward from its rim
	SquareShieldGlowMin      = 0.15
	SquareShieldGlowMax      = 0.45
	SquareShieldGlowPeriodMs = 900
	SquareShieldGlowBand     = 0.35

	// SquareShieldRim256 is the share of the band's glow a console shows as the rim
	SquareShieldRim256 = 0.6
)

// SquareMeterEighths fill a meter's last cell an eighth at a time, from the left
var SquareMeterEighths = [7]rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}
