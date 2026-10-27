package visual

import "github.com/lixenwraith/color"

// StoneUnit is fixed-point one for the stone art: a knot, a cover or a light is a share
// of it, and StoneUnitBits is its shift for color.LerpFixed
const (
	StoneUnitBits = 8
	StoneUnit     = 1 << StoneUnitBits
)

// StonePalette is the rock a stone preset draws in: its background and grain at the two
// ends of its mottle
type StonePalette struct {
	Bg, BgMottle color.RGB
	Fg, FgMottle color.RGB
}

// Each preset's host rock; its grain keeps it visible on a console, whose black takes a
// dark background
var (
	StonePlain = StonePalette{
		Bg: color.RGB{R: 62, G: 58, B: 52}, BgMottle: color.RGB{R: 90, G: 85, B: 76},
		Fg: color.RGB{R: 96, G: 90, B: 81}, FgMottle: color.RGB{R: 124, G: 117, B: 105},
	}
	StoneCracked = StonePalette{
		Bg: color.RGB{R: 60, G: 62, B: 66}, BgMottle: color.RGB{R: 86, G: 88, B: 93},
		Fg: color.RGB{R: 93, G: 96, B: 101}, FgMottle: color.RGB{R: 120, G: 123, B: 129},
	}
	StoneMoss = StonePalette{
		Bg: color.RGB{R: 54, G: 56, B: 48}, BgMottle: color.RGB{R: 76, G: 78, B: 66},
		Fg: color.RGB{R: 85, G: 87, B: 75}, FgMottle: color.RGB{R: 108, G: 110, B: 95},
	}
	StoneCrystal = StonePalette{
		Bg: color.RGB{R: 44, G: 46, B: 56}, BgMottle: color.RGB{R: 62, G: 64, B: 78},
		Fg: color.RGB{R: 74, G: 77, B: 92}, FgMottle: color.RGB{R: 96, G: 99, B: 116},
	}
	// Masonry's blocks range over the background and its mortar is the grain
	StoneMasonry = StonePalette{
		Bg: color.RGB{R: 88, G: 73, B: 59}, BgMottle: color.RGB{R: 112, G: 95, B: 77},
		Fg: color.RGB{R: 130, G: 122, B: 108}, FgMottle: color.RGB{R: 130, G: 122, B: 108},
	}
)

// Stone mottle: knots 1<<StoneMottleColBits columns and 1<<StoneMottleRowBits rows
// apart, twice as many columns for a cell twice as tall, StoneFleck of it from a finer
// octave; each cell's grain sways StoneSpeck, and a face's crust runs up to StoneCrust
// cells deep
const (
	StoneMottleColBits = 4
	StoneMottleRowBits = 3
	StoneFleckColBits  = 2
	StoneFleckRowBits  = 1
	StoneFleck         = StoneUnit / 3
	StoneSpeck         = StoneUnit / 12
	StoneCrust         = 3
)

// StoneGrain and StoneGrainDense are the bare rock's glyphs, and their inks the share of
// a cell each covers
const (
	StoneGrain         = '░'
	StoneGrainDense    = '▒'
	StoneGrainInk      = StoneUnit / 4
	StoneGrainDenseInk = StoneUnit / 2
)

// StoneLitDepth is how far a face brightens the rock, by depth; deeper rock keeps its own
var StoneLitDepth = [...]int64{StoneUnit * 3 / 8, StoneUnit / 4, StoneUnit / 8, StoneUnit / 16}

// Light falls from above, so a face lights by the side it opens on
const (
	StoneLitUp   = StoneUnit
	StoneLitSide = StoneUnit * 13 / 16
	StoneLitDown = StoneUnit * 5 / 8
)

// Strata: bands StrataRows apart, so a group holds one for every StrataRows of its height;
// each boundary strays within StrataJitter of a band, the bands sway StrataWave of a row
// from crest to trough with knots 1<<StrataWaveColBits columns apart, and the rock's
// mottle shades a band StrataMottle
const (
	StrataRows        = 3
	StrataJitter      = StoneUnit * 3 / 8
	StrataWaveColBits = 4
	StrataWave        = StoneUnit * 5 / 2
	StrataMottle      = StoneUnit / 3
)

// StrataBand is one band's tones: its background and its grain over it
type StrataBand struct {
	Bg, Fg color.RGB
	Grain  rune
}

// Light and dark bands alternate, each drawn from its family; bands differ in tone and
// shade density, never in the kind of mark. The light ▒ band inks toward its background
// so its denser shade lifts the band less, as far as its grain still shows in 256 colours.
var (
	StrataLight = [...]StrataBand{
		{Bg: color.RGB{R: 96, G: 85, B: 68}, Fg: color.RGB{R: 126, G: 113, B: 92}, Grain: '░'},
		{Bg: color.RGB{R: 98, G: 94, B: 85}, Fg: color.RGB{R: 128, G: 123, B: 112}, Grain: '░'},
		{Bg: color.RGB{R: 92, G: 80, B: 64}, Fg: color.RGB{R: 118, G: 103, B: 84}, Grain: '▒'},
	}
	StrataDark = [...]StrataBand{
		{Bg: color.RGB{R: 70, G: 66, B: 63}, Fg: color.RGB{R: 90, G: 86, B: 82}, Grain: '░'},
		{Bg: color.RGB{R: 74, G: 66, B: 58}, Fg: color.RGB{R: 102, G: 92, B: 82}, Grain: '░'},
		{Bg: color.RGB{R: 72, G: 64, B: 58}, Fg: color.RGB{R: 100, G: 90, B: 82}, Grain: '▒'},
	}
)

// Cracks: veins walked a cell a step along a row or a column, at most one from each tile
// of CrackTileCols by CrackTileRows with CrackChance, CrackUpright of them down a column;
// a vein runs at least CrackMinSteps and at most a tile, its ends CrackEnd as dark as its
// body CrackTaper steps in
const (
	CrackTileCols = 16
	CrackTileRows = 8
	CrackChance   = StoneUnit * 7 / 8
	CrackUpright  = StoneUnit * 5 / 8
	CrackMinSteps = 4
	CrackTaper    = 2
	CrackEnd      = StoneUnit * 5 / 8
)

// CrackFg is a vein's ink at its darkest
var CrackFg = color.RGB{R: 26, G: 26, B: 29}

// Crack glyphs: along a row a vein runs on the cell's foot, rising or falling a row a step
const (
	CrackFlat  = '_'
	CrackRise  = '/'
	CrackFall  = '\\'
	CrackPlumb = '|'
)

// Moss: the share of cells moss covers by depth on an upward face, the side and downward
// shares of that, in patches with knots 1<<MossColBits columns and 1<<MossRowBits rows
// apart and each cell's cover off by up to MossJitter; past MossThick of cover it grows
// dense, MossSprouts of an upward face's dense cells sprout, and it shades the rock under
// it MossShade of its strength
const (
	MossColBits = 3
	MossRowBits = 2
	MossThick   = StoneUnit / 4
	MossSide    = StoneUnit * 3 / 4
	MossDown    = StoneUnit / 8
	MossJitter  = StoneUnit / 8
	MossSprouts = StoneUnit / 3
	MossShade   = StoneUnit / 2
)

// MossCover is the share of an upward face's cells moss covers, by depth
var MossCover = [...]int64{StoneUnit * 15 / 16, StoneUnit * 5 / 8, StoneUnit * 5 / 16, StoneUnit / 8}

// Moss runs from dark to light with its strength, over the rock shaded toward MossBg
var (
	MossDark  = color.RGB{R: 56, G: 98, B: 32}
	MossLight = color.RGB{R: 92, G: 128, B: 56}
	MossBg    = color.RGB{R: 44, G: 58, B: 34}
)

// Moss glyphs: a sprout at an upward face, dense over thick cover and thin elsewhere
const (
	MossSprout = '"'
	MossDense  = '▒'
	MossThin   = '░'
)

// Crystals: one point at most in each tile of CrystalTileCols by CrystalTileRows, with
// CrystalChance scaled down for a group under CrystalFullArea cells; the cells beside it
// take CrystalHalo of its glow, those a step further half that, the point CrystalCore
const (
	CrystalTileCols = 10
	CrystalTileRows = 5
	CrystalChance   = StoneUnit * 3 / 8
	CrystalFullArea = 900
	CrystalHalo     = StoneUnit / 4
	CrystalCore     = StoneUnit / 2
)

// CrystalHue is one kind of crystal: its points' dimmest and brightest and the glow they
// cast; a group grows one kind
type CrystalHue struct {
	Dim, Bright, Glow color.RGB
}

// CrystalHues are the kinds of crystal: amethyst, aqua and quartz
var CrystalHues = [...]CrystalHue{
	{Dim: color.RGB{R: 150, G: 110, B: 220}, Bright: color.RGB{R: 214, G: 182, B: 255}, Glow: color.RGB{R: 84, G: 64, B: 120}},
	{Dim: color.RGB{R: 80, G: 180, B: 200}, Bright: color.RGB{R: 170, G: 240, B: 250}, Glow: color.RGB{R: 44, G: 92, B: 110}},
	{Dim: color.RGB{R: 200, G: 190, B: 150}, Bright: color.RGB{R: 250, G: 245, B: 215}, Glow: color.RGB{R: 100, G: 96, B: 76}},
}

// CrystalGlyphs are the points' glyphs, drawn evenly, so a doubled one comes twice as often
var CrystalGlyphs = [...]rune{'♦', '♦', '*', '+'}

// Masonry: courses a row high of blocks a group's width over MasonryBlocksAcross long,
// within the bounds, every other course set half a block over, each joint up to
// MasonryJointStray cells off its place, and the rock's mottle shading a block
// MasonryMottle
const (
	MasonryBlocksAcross = 6
	MasonryMinLength    = 4
	MasonryMaxLength    = 12
	MasonryJointStray   = 1
	MasonryMottle       = StoneUnit / 8
	MasonryJoint        = '|'
	MasonryBed          = '_'
)
