package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// Eye animation constraints
const (
	// EyeMaxFrames is the upper bound on animation frames per eye type
	EyeMaxFrames = 5

	// Eye256FlashFg is xterm-256 bright yellow for hit flash
	Eye256FlashFg = color.P256Yellow
)

var (
	// Eye explosion gradient — Palette Set A: Void Purple (active)
	RgbEyeExplosionEdge = color.DarkViolet
	RgbEyeExplosionMid  = color.ElectricViolet
	RgbEyeExplosionCore = color.PaleLavender

	// Palette Set B: Crimson (alternative)
	// RgbEyeExplosionEdge = color.DarkBurgundy
	// RgbEyeExplosionMid  = color.Cinnabar
	// RgbEyeExplosionCore = color.MistyRose

	// Palette Set C: Neon Magenta (alternative)
	// RgbEyeExplosionEdge = color.DeepPurple
	// RgbEyeExplosionMid  = color.HotMagenta
	// RgbEyeExplosionCore = color.PalePink

	// Palette Set D: Void Teal (alternative)
	// RgbEyeExplosionEdge = color.Teal
	// RgbEyeExplosionMid  = color.BrightCyan
	// RgbEyeExplosionCore = color.PaleCyan
)

// EyeFrameArt holds per-frame character art and palette index mappings
// Each row string is EyeWidth characters wide
type EyeFrameArt struct {
	Art  [3]string // Character art per row
	Fg   [3]string // Foreground palette index per cell (hex: 0-9, a-f)
	Bg   [3]string // Background palette index per cell (hex: 0-9, a-f; space = transparent)
	Attr [3]string // Attribute per cell ('B'=Bold, 'D'=Dim, ' '=None)
}

// EyeTypeVisual holds the complete visual specification for one eye type
type EyeTypeVisual struct {
	FgPalette  [8]color.RGB
	BgPalette  [3]color.RGB
	Fg256      uint8
	Bg256      uint8
	FrameCount int
	Frames     [EyeMaxFrames]EyeFrameArt
}

// EyeTypeVisuals indexed by EyeType iota values
var EyeTypeVisuals = [parameter.EyeTypeCount]EyeTypeVisual{

	// 0: Void Eye — 5×3, 5 frames
	// Deep ocean, slow blink cycle (O→o→=→O→shut)
	{
		FgPalette: [8]color.RGB{
			color.DimGray, color.SteelBlue, color.White,
			color.CeruleanBlue, color.NavyBlue, color.LightSkyBlue,
			color.CobaltBlue, color.DodgerBlue,
		},
		BgPalette: [3]color.RGB{
			color.DeepNavy, color.Gunmetal, color.CobaltBlue,
		},
		Fg256: color.P256SteelBlue, Bg256: color.P256DeepNavy,
		FrameCount: 5,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{"[---]", "|(O)|", "[---]"},
				Fg:   [3]string{"01110", "43234", "01110"},
				Bg:   [3]string{"00000", "01210", "00000"},
				Attr: [3]string{" BBB ", " BBB ", " BBB "},
			},
			{
				Art:  [3]string{"[---]", "|(o)|", "[---]"},
				Fg:   [3]string{"01110", "43534", "01110"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{" BBB ", " BBB ", " BBB "},
			},
			{
				Art:  [3]string{"[===]", "|(=)|", "[===]"},
				Fg:   [3]string{"06660", "43634", "06660"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{" BBB ", "  B  ", " BBB "},
			},
			{
				Art:  [3]string{"[~~~]", "|(O)|", "[~~~]"},
				Fg:   [3]string{"07770", "43234", "07770"},
				Bg:   [3]string{"00100", "01210", "00100"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Art:  [3]string{"[---]", "|===|", "[---]"},
				Fg:   [3]string{"01110", "46664", "01110"},
				Bg:   [3]string{"00000", "00000", "00000"},
				Attr: [3]string{" BBB ", "     ", " BBB "},
			},
		},
	},

	// 1: Flame Eye — 5×3, 4 frames
	// Aggressive flicker (<@>→{*}→<o>→<O>)
	{
		FgPalette: [8]color.RGB{
			color.LemonYellow, color.FlameOrange, color.White,
			color.BrightRed, color.Amber, color.DarkCrimson,
			color.Vermilion, color.WarmOrange,
		},
		BgPalette: [3]color.RGB{
			color.BlackRed, color.DarkAmber, color.Red,
		},
		Fg256: color.P256Orange, Bg256: color.P256DarkCrimson,
		FrameCount: 4,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{"#---#", "|<@>|", "#---#"},
				Fg:   [3]string{"51115", "54245", "51115"},
				Bg:   [3]string{"00000", "01210", "00000"},
				Attr: [3]string{"B   B", " BBB ", "B   B"},
			},
			{
				Art:  [3]string{"#-#-#", "|{*}|", "#-#-#"},
				Fg:   [3]string{"51615", "57275", "51615"},
				Bg:   [3]string{"01010", "01210", "01010"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Art:  [3]string{"#---#", "|<o>|", "#---#"},
				Fg:   [3]string{"51115", "54745", "51115"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"B   B", " BBB ", "B   B"},
			},
			{
				Art:  [3]string{"#===#", "|<O>|", "#===#"},
				Fg:   [3]string{"50005", "54245", "50005"},
				Bg:   [3]string{"01110", "01210", "01110"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 2: Frost Eye — 5×3, 4 frames
	// Crystalline pulse (<O>→(O)→{=}→(O))
	{
		FgPalette: [8]color.RGB{
			color.BrightCyan, color.White, color.LightSkyBlue,
			color.CeruleanBlue, color.SteelBlue, color.CoolSilver,
			color.AliceBlue, color.PaleCyan,
		},
		BgPalette: [3]color.RGB{
			color.DeepNavy, color.CobaltBlue, color.SteelBlue,
		},
		Fg256: color.P256LightBlue, Bg256: color.P256DarkBlue,
		FrameCount: 4,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{"*---*", "|<O>|", "*---*"},
				Fg:   [3]string{"43334", "30103", "43334"},
				Bg:   [3]string{"00000", "01210", "00000"},
				Attr: [3]string{"B   B", " BBB ", "B   B"},
			},
			{
				Art:  [3]string{"*-+-*", "|(O)|", "*-+-*"},
				Fg:   [3]string{"43134", "30103", "43134"},
				Bg:   [3]string{"00100", "01210", "00100"},
				Attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Art:  [3]string{"*---*", "|{=}|", "*---*"},
				Fg:   [3]string{"43334", "30534", "43334"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"B   B", "  B  ", "B   B"},
			},
			{
				Art:  [3]string{"*~+~*", "|(O)|", "*~+~*"},
				Fg:   [3]string{"40104", "30103", "40104"},
				Bg:   [3]string{"01210", "01210", "01210"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 3: Storm Eye — 5×3, 3 frames
	// Electric, rotating highlight
	{
		FgPalette: [8]color.RGB{
			color.BrightCyan, color.CeruleanBlue, color.White,
			color.LemonYellow, color.SteelBlue, color.DodgerBlue,
			color.SkyTeal, color.LightSkyBlue,
		},
		BgPalette: [3]color.RGB{
			color.DeepNavy, color.CobaltBlue, {},
		},
		Fg256: color.P256Cyan, Bg256: color.P256DeepNavy,
		FrameCount: 3,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{"+~~~+", "|(O)|", "+~~~+"},
				Fg:   [3]string{"40004", "41214", "40004"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Art:  [3]string{"+~~~+", "|(=)|", "+~~~+"},
				Fg:   [3]string{"40004", "41614", "40004"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"BBBBB", " B B ", "BBBBB"},
			},
			{
				Art:  [3]string{"+~~~+", "|{O}|", "+~~~+"},
				Fg:   [3]string{"43034", "43234", "43034"},
				Bg:   [3]string{"00100", "01110", "00100"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 4: Blood Eye — 5×3, 4 frames
	// Veined pulse (X pupil, dilate cycle)
	{
		FgPalette: [8]color.RGB{
			color.DarkCrimson, color.BrightRed, color.White,
			color.Vermilion, color.Coral, color.Red,
			color.Salmon, color.LightCoral,
		},
		BgPalette: [3]color.RGB{
			color.BlackRed, color.DarkCrimson, color.Red,
		},
		Fg256: color.P256Crimson, Bg256: color.P256Maroon,
		FrameCount: 4,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{">---<", "|(X)|", ">---<"},
				Fg:   [3]string{"31113", "05250", "31113"},
				Bg:   [3]string{"00000", "01210", "00000"},
				Attr: [3]string{"B   B", " BBB ", "B   B"},
			},
			{
				Art:  [3]string{">===<", "|(X)|", ">===<"},
				Fg:   [3]string{"35553", "05250", "35553"},
				Bg:   [3]string{"01110", "01210", "01110"},
				Attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Art:  [3]string{">---<", "|-X-|", ">---<"},
				Fg:   [3]string{"31113", "05250", "31113"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"B   B", "  B  ", "B   B"},
			},
			{
				Art:  [3]string{">-#-<", "|(O)|", ">-#-<"},
				Fg:   [3]string{"31513", "04240", "31513"},
				Bg:   [3]string{"00100", "01210", "00100"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 5: Golden Eye — 5×3, 4 frames
	// Regal shimmer, warm amber
	{
		FgPalette: [8]color.RGB{
			color.Gold, color.Amber, color.White,
			color.LemonYellow, color.DarkGold, color.PaleGold,
			color.Buttercream, color.WarmOrange,
		},
		BgPalette: [3]color.RGB{
			color.DarkAmber, color.Amber, color.Gold,
		},
		Fg256: color.P256Gold, Bg256: color.P256DarkAmber,
		FrameCount: 4,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{"|===|", "|(O)|", "|===|"},
				Fg:   [3]string{"40004", "41214", "40004"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Art:  [3]string{"|=#=|", "|{O}|", "|=#=|"},
				Fg:   [3]string{"40304", "71217", "40304"},
				Bg:   [3]string{"00100", "01110", "00100"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Art:  [3]string{"|===|", "|(=)|", "|===|"},
				Fg:   [3]string{"40004", "41514", "40004"},
				Bg:   [3]string{"00000", "01110", "00000"},
				Attr: [3]string{"BBBBB", " B B ", "BBBBB"},
			},
			{
				Art:  [3]string{"|~#~|", "|(O)|", "|~#~|"},
				Fg:   [3]string{"43334", "41214", "43334"},
				Bg:   [3]string{"01210", "01210", "01210"},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 6: Abyss Eye — 5×3, 4 frames
	// Transparent corners (bg skip), dimensional rift
	{
		FgPalette: [8]color.RGB{
			color.PaleLavender, color.ElectricViolet, color.White,
			color.SoftLavender, color.DarkViolet, color.MutedPurple,
			color.DeepPurple, color.Orchid,
		},
		BgPalette: [3]color.RGB{
			color.Obsidian, color.DeepPurple, {},
		},
		Fg256: color.P256MediumPurple, Bg256: color.P256DarkPurpleBlue,
		FrameCount: 4,
		Frames: [EyeMaxFrames]EyeFrameArt{
			{
				Art:  [3]string{".---.", "|(O)|", "'---'"},
				Fg:   [3]string{"64446", "41214", "64446"},
				Bg:   [3]string{" 000 ", "01110", " 000 "},
				Attr: [3]string{" BBB ", " BBB ", " BBB "},
			},
			{
				Art:  [3]string{".---.", "|{O}|", "'---'"},
				Fg:   [3]string{"64446", "47274", "64446"},
				Bg:   [3]string{" 000 ", "01110", " 000 "},
				Attr: [3]string{" BBB ", " BBB ", " BBB "},
			},
			{
				Art:  [3]string{".~~~.", "|[O]|", "'~~~'"},
				Fg:   [3]string{"65556", "41214", "65556"},
				Bg:   [3]string{" 111 ", "01110", " 111 "},
				Attr: [3]string{"DBBBD", " BBB ", "DBBBD"},
			},
			{
				Art:  [3]string{".~~~.", "|(O)|", "'~~~'"},
				Fg:   [3]string{"61116", "41214", "61116"},
				Bg:   [3]string{" 111 ", "01110", " 111 "},
				Attr: [3]string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},
}
