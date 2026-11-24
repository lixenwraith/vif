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
	component.SquareWhirl:   {Block: color.Gold, Glow: color.Apricot, Track: color.DarkGold, Console: ConYellow},
	component.SquareDefense: {Block: color.BrightCyan, Glow: color.SkyTeal, Track: color.Teal, Console: ConCyan},
}

// SquareMagic and SquareBurst are the legend entries over the square, its magic
// meter and its cursor's overheat, each drawn as a kind's
var (
	SquareMagic = SquareKindColors{Block: color.White, Glow: color.White, Track: color.Gray, Console: ConBrightWhite}
	SquareBurst = SquareKindColors{Block: color.ElectricViolet, Glow: color.PaleLavender, Track: color.DarkViolet, Console: ConBrightMagenta}
)

// SquareOrb is the orb of a kind that strikes, on the square's outer orbit: its
// glyph's color, the two its corona turns in, and a console's
type SquareOrb struct {
	Core, Lobe, Counter color.RGB
	Console             uint8
}

// SquareOrbs holds each striking kind's orb: red, white and blue for the attack
var SquareOrbs = [component.SquareKindCount]SquareOrb{
	component.SquareAttack: {Core: color.White, Lobe: color.Red, Counter: color.RoyalBlue, Console: ConBrightRed},
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

	// A boosting kind lights the shield's rim while its boost holds: a band
	// SquareSurgeWidth columns deep, at most SquareSurgeAlpha of the kind's glow,
	// heaviest in two lobes that turn once each SquareSurgeTurn
	SquareSurgeWidth = 6.0
	SquareSurgeAlpha = 0.95
	SquareSurgeTurn  = 700 * time.Millisecond

	// A strike that landed tints the part of the map its line faces in the kind's
	// glow, at most SquareStrikeAlpha, fading over SquareStrikeFlash
	SquareStrikeFlash = 250 * time.Millisecond
	SquareStrikeAlpha = 0.15

	// A turning wind draws SquareWhirlStreaks streaks on the outer orbit while it
	// blows, each SquareWhirlTail columns fading back from a head of at most
	// SquareWhirlAlpha of the kind's glow, round the orbit once each SquareWhirlTurn
	SquareWhirlStreaks = 4
	SquareWhirlTail    = 22
	SquareWhirlAlpha   = 0.8
	SquareWhirlTurn    = 2400 * time.Millisecond
)

// SquareMeterEighths fill a meter's last cell an eighth at a time, from the left
var SquareMeterEighths = [7]rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}
