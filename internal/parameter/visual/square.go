package visual

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
)

// SquareKindColors are what a magic square's blocks of one kind draw in: a block,
// the glow its line fires and its effect holds in, the legend's empty meter, and a
// console's color
type SquareKindColors struct {
	Block, Glow, Track color.RGB
	Console            uint8
}

// SquareKinds holds each kind's colors
var SquareKinds = [component.SquareKindCount]SquareKindColors{
	component.SquareAttack:  {Block: color.Vermilion, Glow: color.Coral, Track: color.DarkCrimson, Console: ConRed},
	component.SquareHold:    {Block: color.Gold, Glow: color.Apricot, Track: color.DarkGold, Console: ConYellow},
	component.SquareDefense: {Block: color.BrightCyan, Glow: color.SkyTeal, Track: color.Teal, Console: ConCyan},
}

// SquareMagic is the magic state's legend entry over the square, drawn as a kind's
var SquareMagic = SquareKindColors{Block: color.White, Glow: color.White, Track: color.Gray, Console: ConBrightWhite}

// SquareOrb is an aiming kind's orb on the square's outer orbit: its glyph's color,
// the two its corona turns in, and a console's
type SquareOrb struct {
	Core, Lobe, Counter color.RGB
	Console             uint8
}

// SquareOrbs holds each aiming kind's orb: red, white and blue for the attack, and
// for the hold the gold, cyan and purple of the game's bolts
var SquareOrbs = [component.SquareKindCount]SquareOrb{
	component.SquareAttack: {Core: color.White, Lobe: color.Red, Counter: color.RoyalBlue, Console: ConBrightRed},
	component.SquareHold:   {Core: color.VibrantCyan, Lobe: color.Gold, Counter: color.MediumPurple, Console: ConBrightYellow},
}

// The square's marks: a block's digit, the frame round the viewer's selection, and
// in the legend under it a kind no line holds
var (
	RgbSquareDigit = color.Black
	RgbSquareFrame = color.White
	RgbSquareIdle  = color.Gray
)

const (
	// SquareMeterEmpty256 is a console's empty meter cell
	SquareMeterEmpty256 = '·'

	// SquareFireFlash is how long a kind's blocks take to fade from its glow as its
	// line fires
	SquareFireFlash = 400 * time.Millisecond

	// SquareMeterTrack is how far a legend meter's empty cells stand from the
	// background toward the kind's track color
	SquareMeterTrack = 0.3

	// SquareOrbIdle is how bright the orb of a kind no line holds draws, of a held one's
	SquareOrbIdle = 0.4

	// A gust draws as a ring SquareGustWidth columns either side of its radius, at
	// most SquareGustAlpha of its kind's glow
	SquareGustWidth = 1.5
	SquareGustAlpha = 0.6
)

// SquareMeterEighths fill a meter's last cell an eighth at a time, from the left
var SquareMeterEighths = [7]rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}
