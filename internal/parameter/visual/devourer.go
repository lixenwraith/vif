package visual

import "github.com/lixenwraith/color"

const (
	// Folds run along the walls DevourerFoldWave apart in depth, drifting outward at
	// DevourerFoldSpeed radians a second, bent by DevourerFoldWarp of the warp field.
	// A wet lip DevourerLipWidth deep shines at the wall, and past DevourerDeepFrom
	// of depth the flesh darkens over DevourerDeepSpan more.
	DevourerFoldWave  = 0.95
	DevourerFoldSpeed = 0.9
	DevourerFoldWarp  = 3.2
	DevourerLipWidth  = 1.6
	DevourerDeepFrom  = 6.0
	DevourerDeepSpan  = 20.0

	// The flesh beats DevourerPulseRate times a second by DevourerPulse of its light
	DevourerPulseRate = 0.7
	DevourerPulse     = 0.06

	// The texture's fields in cycles a column unit, warp, grain and veins, the vein's
	// crest narrowed by DevourerVeinSharp
	DevourerWarpScale  = 0.05
	DevourerGrainScale = 0.18
	DevourerVeinScale  = 0.09
	DevourerVeinSharp  = 18.0

	// A gut's segments swell every DevourerGutSegment column units along it, crawling
	// at DevourerGutCrawl radians a second, a gut DevourerGutBulk times its hitbox's
	// radius at a segment's widest
	DevourerGutSegment = 3.4
	DevourerGutCrawl   = 2.0
	DevourerGutBulk    = 1.25

	// A hit lights a gut DevourerGutHitFlash of the way to the hit flash's color
	// within DevourerGutHitCore column units of where it landed, fading out to
	// DevourerGutHitReach
	DevourerGutHitFlash = 0.85
	DevourerGutHitCore  = 2.5
	DevourerGutHitReach = 7.0

	// An omen's core, DevourerOmenCore column units either side of its ray, rises
	// from DevourerOmenCoreMin to DevourerOmenCoreMax of the attack color as its gut
	// nears, beating faster; its ends glow DevourerOmenEnd round, and its halo, out
	// to DevourerOmenHalo, as a charge's landing does, KrakenLandingGlowMin to Max
	DevourerOmenCore    = 1.0
	DevourerOmenCoreMin = 0.45
	DevourerOmenCoreMax = 0.9
	DevourerOmenEnd     = 2.5
	DevourerOmenHalo    = 3.2
)

// RgbDevourerFlesh is the flesh's ramp from its deepest shadow to a wet highlight
var RgbDevourerFlesh = [...]struct {
	At    float64
	Color color.RGB
}{
	{0, color.RGB{R: 16, G: 2, B: 6}},
	{0.25, color.RGB{R: 58, G: 6, B: 16}},
	{0.5, color.RGB{R: 116, G: 16, B: 28}},
	{0.7, color.RGB{R: 168, G: 44, B: 50}},
	{0.88, color.RGB{R: 212, G: 100, B: 88}},
	{1, color.RGB{R: 255, G: 178, B: 150}},
}

var (
	RgbDevourerVein      = color.RGB{R: 46, G: 4, B: 40}
	RgbDevourerGloss     = color.RGB{R: 255, G: 210, B: 190}
	RgbDevourerGut       = color.RGB{R: 222, G: 104, B: 108} // its lit crest
	RgbDevourerGutFlank  = color.RGB{R: 70, G: 8, B: 22}
	RgbDevourerGutCrease = color.RGB{R: 36, G: 2, B: 12}
	RgbDevourerGutRim    = color.RGB{R: 30, G: 2, B: 8}
	RgbDevourerOmen      = RgbKrakenAttack
)

// Devourer256Tiers are the whole cells a console shades the flesh and the guts in,
// each standing for the true color nearest the cell's: the body red, darkened by
// shade runes in its folds, its lip bright, its veins magenta and its guts pink.
// Backgrounds keep to the eight a console shows.
var Devourer256Tiers = [...]struct {
	Color  color.RGB
	Rune   rune
	Fg, Bg uint8
}{
	{color.RGB{R: 24, G: 3, B: 8}, '▒', ConBlack, ConRed},
	{color.RGB{R: 70, G: 8, B: 18}, '░', ConBlack, ConRed},
	{color.RGB{R: 120, G: 18, B: 30}, ' ', ConRed, ConRed},
	{color.RGB{R: 170, G: 46, B: 50}, '░', ConBrightRed, ConRed},
	{color.RGB{R: 226, G: 130, B: 110}, '▒', ConBrightRed, ConRed},
	{color.RGB{R: 46, G: 4, B: 40}, '▒', ConMagenta, ConBlack},
	{color.RGB{R: 222, G: 104, B: 108}, '▒', ConBrightMagenta, ConMagenta},
}

// Devourer256Gut is a cut gut's fading ground, Devourer256Omen an omen's core, and
// Devourer256Hit over Devourer256HitRune a gut's cells where a hit just landed
const (
	Devourer256Gut     = ConMagenta
	Devourer256Omen    = ConRed
	Devourer256Hit     = ConBrightYellow
	Devourer256HitRune = '▒'
)
