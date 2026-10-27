package visual

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/parameter"
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

// EyeTypeVisuals are the eye types' looks, indexed by EyeType; the console pair
// reads on every console palette
var EyeTypeVisuals = [parameter.EyeTypeCount]LookArt{

	// 0: Void Eye — 5×3, 5 frames
	// Deep ocean, slow blink cycle (O→o→=→O→shut)
	{
		Fg: []color.RGB{
			color.DimGray, color.SteelBlue, color.White,
			color.CeruleanBlue, color.NavyBlue, color.LightSkyBlue,
			color.CobaltBlue, color.DodgerBlue,
		},
		Bg: []color.RGB{
			color.DeepNavy, color.Gunmetal, color.CobaltBlue,
		},
		Fg256: ConWhite, Bg256: ConBlue, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{"[---]", "|(O)|", "[---]"},
				Fg:     []string{"01110", "43234", "01110"},
				Bg:     []string{"00000", "01210", "00000"},
				Attr:   []string{" BBB ", " BBB ", " BBB "},
			},
			{
				Glyphs: []string{"[---]", "|(o)|", "[---]"},
				Fg:     []string{"01110", "43534", "01110"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{" BBB ", " BBB ", " BBB "},
			},
			{
				Glyphs: []string{"[===]", "|(=)|", "[===]"},
				Fg:     []string{"06660", "43634", "06660"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{" BBB ", "  B  ", " BBB "},
			},
			{
				Glyphs: []string{"[~~~]", "|(O)|", "[~~~]"},
				Fg:     []string{"07770", "43234", "07770"},
				Bg:     []string{"00100", "01210", "00100"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Glyphs: []string{"[---]", "|===|", "[---]"},
				Fg:     []string{"01110", "46664", "01110"},
				Bg:     []string{"00000", "00000", "00000"},
				Attr:   []string{" BBB ", "     ", " BBB "},
			},
		},
	},

	// 1: Flame Eye — 5×3, 4 frames
	// Aggressive flicker (<@>→{*}→<o>→<O>)
	{
		Fg: []color.RGB{
			color.LemonYellow, color.FlameOrange, color.White,
			color.BrightRed, color.Amber, color.DarkCrimson,
			color.Vermilion, color.WarmOrange,
		},
		Bg: []color.RGB{
			color.BlackRed, color.DarkAmber, color.Red,
		},
		Fg256: ConBrightYellow, Bg256: ConRed, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{"#---#", "|<@>|", "#---#"},
				Fg:     []string{"51115", "54245", "51115"},
				Bg:     []string{"00000", "01210", "00000"},
				Attr:   []string{"B   B", " BBB ", "B   B"},
			},
			{
				Glyphs: []string{"#-#-#", "|{*}|", "#-#-#"},
				Fg:     []string{"51615", "57275", "51615"},
				Bg:     []string{"01010", "01210", "01010"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Glyphs: []string{"#---#", "|<o>|", "#---#"},
				Fg:     []string{"51115", "54745", "51115"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"B   B", " BBB ", "B   B"},
			},
			{
				Glyphs: []string{"#===#", "|<O>|", "#===#"},
				Fg:     []string{"50005", "54245", "50005"},
				Bg:     []string{"01110", "01210", "01110"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 2: Frost Eye — 5×3, 4 frames
	// Crystalline pulse (<O>→(O)→{=}→(O))
	{
		Fg: []color.RGB{
			color.BrightCyan, color.White, color.LightSkyBlue,
			color.CeruleanBlue, color.SteelBlue, color.CoolSilver,
			color.AliceBlue, color.PaleCyan,
		},
		Bg: []color.RGB{
			color.DeepNavy, color.CobaltBlue, color.SteelBlue,
		},
		Fg256: ConBrightWhite, Bg256: ConCyan, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{"*---*", "|<O>|", "*---*"},
				Fg:     []string{"43334", "30103", "43334"},
				Bg:     []string{"00000", "01210", "00000"},
				Attr:   []string{"B   B", " BBB ", "B   B"},
			},
			{
				Glyphs: []string{"*-+-*", "|(O)|", "*-+-*"},
				Fg:     []string{"43134", "30103", "43134"},
				Bg:     []string{"00100", "01210", "00100"},
				Attr:   []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Glyphs: []string{"*---*", "|{=}|", "*---*"},
				Fg:     []string{"43334", "30534", "43334"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"B   B", "  B  ", "B   B"},
			},
			{
				Glyphs: []string{"*~+~*", "|(O)|", "*~+~*"},
				Fg:     []string{"40104", "30103", "40104"},
				Bg:     []string{"01210", "01210", "01210"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 3: Storm Eye — 5×3, 3 frames
	// Electric, rotating highlight
	{
		Fg: []color.RGB{
			color.BrightCyan, color.CeruleanBlue, color.White,
			color.LemonYellow, color.SteelBlue, color.DodgerBlue,
			color.SkyTeal, color.LightSkyBlue,
		},
		Bg: []color.RGB{
			color.DeepNavy, color.CobaltBlue, {},
		},
		Fg256: ConBrightCyan, Bg256: ConBlue, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{"+~~~+", "|(O)|", "+~~~+"},
				Fg:     []string{"40004", "41214", "40004"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Glyphs: []string{"+~~~+", "|(=)|", "+~~~+"},
				Fg:     []string{"40004", "41614", "40004"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"BBBBB", " B B ", "BBBBB"},
			},
			{
				Glyphs: []string{"+~~~+", "|{O}|", "+~~~+"},
				Fg:     []string{"43034", "43234", "43034"},
				Bg:     []string{"00100", "01110", "00100"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 4: Blood Eye — 5×3, 4 frames
	// Veined pulse (X pupil, dilate cycle)
	{
		Fg: []color.RGB{
			color.DarkCrimson, color.BrightRed, color.White,
			color.Vermilion, color.Coral, color.Red,
			color.Salmon, color.LightCoral,
		},
		Bg: []color.RGB{
			color.BlackRed, color.DarkCrimson, color.Red,
		},
		Fg256: ConBrightWhite, Bg256: ConRed, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{">---<", "|(X)|", ">---<"},
				Fg:     []string{"31113", "05250", "31113"},
				Bg:     []string{"00000", "01210", "00000"},
				Attr:   []string{"B   B", " BBB ", "B   B"},
			},
			{
				Glyphs: []string{">===<", "|(X)|", ">===<"},
				Fg:     []string{"35553", "05250", "35553"},
				Bg:     []string{"01110", "01210", "01110"},
				Attr:   []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Glyphs: []string{">---<", "|-X-|", ">---<"},
				Fg:     []string{"31113", "05250", "31113"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"B   B", "  B  ", "B   B"},
			},
			{
				Glyphs: []string{">-#-<", "|(O)|", ">-#-<"},
				Fg:     []string{"31513", "04240", "31513"},
				Bg:     []string{"00100", "01210", "00100"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 5: Golden Eye — 5×3, 4 frames
	// Regal shimmer, warm amber
	{
		Fg: []color.RGB{
			color.Gold, color.Amber, color.White,
			color.LemonYellow, color.DarkGold, color.PaleGold,
			color.Buttercream, color.WarmOrange,
		},
		Bg: []color.RGB{
			color.DarkAmber, color.Amber, color.Gold,
		},
		Fg256: ConBlack, Bg256: ConYellow, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{"|===|", "|(O)|", "|===|"},
				Fg:     []string{"40004", "41214", "40004"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{
				Glyphs: []string{"|=#=|", "|{O}|", "|=#=|"},
				Fg:     []string{"40304", "71217", "40304"},
				Bg:     []string{"00100", "01110", "00100"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{
				Glyphs: []string{"|===|", "|(=)|", "|===|"},
				Fg:     []string{"40004", "41514", "40004"},
				Bg:     []string{"00000", "01110", "00000"},
				Attr:   []string{"BBBBB", " B B ", "BBBBB"},
			},
			{
				Glyphs: []string{"|~#~|", "|(O)|", "|~#~|"},
				Fg:     []string{"43334", "41214", "43334"},
				Bg:     []string{"01210", "01210", "01210"},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// 6: Abyss Eye — 5×3, 4 frames
	// Transparent corners (bg skip), dimensional rift
	{
		Fg: []color.RGB{
			color.PaleLavender, color.ElectricViolet, color.White,
			color.SoftLavender, color.DarkViolet, color.MutedPurple,
			color.DeepPurple, color.Orchid,
		},
		Bg: []color.RGB{
			color.Obsidian, color.DeepPurple, {},
		},
		Fg256: ConBrightMagenta, Bg256: ConMagenta, Console: true,
		Frames: []LookFrame{
			{
				Glyphs: []string{".---.", "|(O)|", "'---'"},
				Fg:     []string{"64446", "41214", "64446"},
				Bg:     []string{" 000 ", "01110", " 000 "},
				Attr:   []string{" BBB ", " BBB ", " BBB "},
			},
			{
				Glyphs: []string{".---.", "|{O}|", "'---'"},
				Fg:     []string{"64446", "47274", "64446"},
				Bg:     []string{" 000 ", "01110", " 000 "},
				Attr:   []string{" BBB ", " BBB ", " BBB "},
			},
			{
				Glyphs: []string{".~~~.", "|[O]|", "'~~~'"},
				Fg:     []string{"65556", "41214", "65556"},
				Bg:     []string{" 111 ", "01110", " 111 "},
				Attr:   []string{"DBBBD", " BBB ", "DBBBD"},
			},
			{
				Glyphs: []string{".~~~.", "|(O)|", "'~~~'"},
				Fg:     []string{"61116", "41214", "61116"},
				Bg:     []string{" 111 ", "01110", " 111 "},
				Attr:   []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},
}
