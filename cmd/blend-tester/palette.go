package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// PaletteEntry represents a named color from the game
type PaletteEntry struct {
	Name  string
	Color render.RGB
	Group string
}

var gamePalette = []PaletteEntry{
	// Sequences - Green
	{"SeqGreenDark", visual.RgbGlyphGreenDark, "Sequence"},
	{"SeqGreenNormal", visual.RgbGlyphGreenNormal, "Sequence"},
	{"SeqGreenBright", visual.RgbGlyphGreenBright, "Sequence"},
	// Sequences - Red
	{"SeqRedDark", visual.RgbGlyphRedDark, "Sequence"},
	{"SeqRedNormal", visual.RgbGlyphRedNormal, "Sequence"},
	{"SeqRedBright", visual.RgbGlyphRedBright, "Sequence"},
	// Sequences - Blue
	{"SeqBlueDark", visual.RgbGlyphBlueDark, "Sequence"},
	{"SeqBlueNormal", visual.RgbGlyphBlueNormal, "Sequence"},
	{"SeqBlueBright", visual.RgbGlyphBlueBright, "Sequence"},
	// Sequences - Special
	{"SeqGold", visual.RgbGlyphGold, "Sequence"},

	// Effects
	{"Decay", visual.RgbDecay, "Effect"},
	{"Blossom", visual.RgbBlossom, "Effect"},
	{"Drain", visual.RgbDrain, "Effect"},
	{"Materialize", visual.RgbMaterialize, "Effect"},
	{"RemovalFlash", visual.RgbRemovalFlash, "Effect"},
	{"CleanerBase", visual.RgbCleanerBasePositive, "Effect"},
	{"ShieldBase", visual.RgbQuasarShield, "Effect"},

	// Cursor/Ping
	{"PingHighlight", visual.RgbPingHighlight, "Cursor"},
	{"PingNormal", visual.RgbPingLineNormal, "Cursor"},
	{"PingOrange", visual.RgbPingOrange, "Cursor"},
	{"PingGreen", visual.RgbPingGreen, "Cursor"},
	{"PingRed", visual.RgbPingRed, "Cursor"},
	{"PingBlue", visual.RgbPingBlue, "Cursor"},
	{"CursorNormal", visual.RgbCursorNormal, "Cursor"},
	{"CursorInsert", visual.RgbCursorInsert, "Cursor"},
	{"CursorError", visual.RgbCursorError, "Cursor"},

	// UI
	{"LineNumbers", visual.RgbRowIndicator, "UI"},
	{"StatusBar", visual.RgbStatusBar, "UI"},
	{"ColumnIndicator", visual.RgbColumnIndicator, "UI"},
	{"Background", visual.RgbBackground, "UI"},
	{"StatusText", visual.RgbStatusText, "UI"},

	// Nugget
	{"NuggetOrange", visual.RgbNuggetOrange, "Nugget"},
	{"NuggetDark", visual.RgbNuggetDark, "Nugget"},
	{"TrailGray", visual.RgbTrailGray, "Nugget"},

	// Mode Backgrounds
	{"ModeNormalBg", visual.RgbModeNormalBg, "ModeBg"},
	{"ModeInsertBg", visual.RgbModeInsertBg, "ModeBg"},
	{"ModeSearchBg", visual.RgbModeSearchBg, "ModeBg"},
	{"ModeCommandBg", visual.RgbModeCommandBg, "ModeBg"},

	// Energy
	{"EnergyBg", visual.RgbEnergyBg, "Energy"},
	{"EnergyBlinkBlue", visual.RgbEnergyBlinkBlue, "Energy"},
	{"EnergyBlinkGreen", visual.RgbEnergyBlinkGreen, "Energy"},
	{"EnergyBlinkRed", visual.RgbEnergyBlinkRed, "Energy"},
	{"EnergyBlinkWhite", visual.RgbEnergyBlinkWhite, "Energy"},

	// Timer Backgrounds
	{"BoostBg", visual.RgbBoostBg, "Timer"},
	{"FpsBg", visual.RgbFpsBg, "Timer"},
	{"GtBg", visual.RgbGtBg, "Timer"},
	{"ApmBg", visual.RgbApmBg, "Timer"},

	// Audio
	{"AudioMuted", visual.RgbAudioBothOff, "Audio"},
	{"AudioUnmuted", visual.RgbAudioBothOn, "Audio"},

	// Overlay
	{"OverlayBorder", visual.RgbOverlayBorder, "Overlay"},
	{"OverlayBg", visual.RgbOverlayBg, "Overlay"},
	{"OverlayText", visual.RgbOverlayText, "Overlay"},
	{"OverlayTitle", visual.RgbOverlayTitle, "Overlay"},

	// General
	{"Black", visual.RgbBlack, "General"},
}

func handlePaletteInput(ev terminal.Event) {
	maxIdx := len(gamePalette) - 1

	switch ev.Key {
	case terminal.KeyUp:
		if state.paletteIdx > 0 {
			state.paletteIdx--
		}
	case terminal.KeyDown:
		if state.paletteIdx < maxIdx {
			state.paletteIdx++
		}
	case terminal.KeyPageUp:
		state.paletteIdx -= 10
		if state.paletteIdx < 0 {
			state.paletteIdx = 0
		}
	case terminal.KeyPageDown:
		state.paletteIdx += 10
		if state.paletteIdx > maxIdx {
			state.paletteIdx = maxIdx
		}
	case terminal.KeyHome:
		state.paletteIdx = 0
	case terminal.KeyEnd:
		state.paletteIdx = maxIdx
	}
}

func drawPaletteMode() {
	startY := 2
	fg := render.RGB{180, 180, 180}
	bg := render.RGB{20, 20, 30}
	dimFg := render.RGB{120, 120, 120}

	// Keys help
	drawText(1, startY, "↑↓:Select PgUp/Dn:Jump Home/End:First/Last", render.RGB{100, 100, 100}, bg)
	startY += 2

	// Column headers
	drawText(1, startY, "##", dimFg, bg)
	drawText(5, startY, "Group", dimFg, bg)
	drawText(16, startY, "Name", dimFg, bg)
	drawText(35, startY, "TC", render.RGB{100, 180, 100}, bg)
	drawText(40, startY, "256", render.RGB{180, 180, 100}, bg)
	drawText(45, startY, "RGB (decimal)", dimFg, bg)
	drawText(60, startY, "Hex", dimFg, bg)
	drawText(69, startY, "256 idx", dimFg, bg)
	startY++

	// Separator line
	for x := 1; x < 80; x++ {
		drawSwatchChar(x, startY, '─', render.RGB{60, 60, 60}, bg)
	}
	startY++

	// Calculate visible rows
	listHeight := state.height - startY - 10
	if listHeight < 5 {
		listHeight = 5
	}

	// Adjust scroll
	if state.paletteIdx < state.paletteScroll {
		state.paletteScroll = state.paletteIdx
	}
	if state.paletteIdx >= state.paletteScroll+listHeight {
		state.paletteScroll = state.paletteIdx - listHeight + 1
	}

	// Draw palette list
	for i := 0; i < listHeight && state.paletteScroll+i < len(gamePalette); i++ {
		idx := state.paletteScroll + i
		entry := gamePalette[idx]
		y := startY + i

		// Selection highlight
		rowBg := bg
		if idx == state.paletteIdx {
			rowBg = render.RGB{60, 60, 80}
		}

		// Clear row
		for x := 0; x < state.width; x++ {
			buf.SetWithBg(x, y, ' ', fg, rowBg)
		}

		// Index
		drawText(1, y, fmt.Sprintf("%2d", idx), render.RGB{100, 100, 100}, rowBg)

		// Group
		drawText(5, y, fmt.Sprintf("%-10s", entry.Group), render.RGB{120, 120, 120}, rowBg)

		// Name
		drawText(16, y, fmt.Sprintf("%-18s", entry.Name), fg, rowBg)

		// TC Swatch
		drawSwatch(35, y, 4, entry.Color)

		// 256 Swatch (Redmean)
		idx256 := terminal.RGBTo256(entry.Color)
		rgb256 := Get256PaletteRGB(idx256)
		drawSwatch(40, y, 4, rgb256)

		// RGB values
		drawText(45, y, fmt.Sprintf("(%3d,%3d,%3d)", entry.Color.R, entry.Color.G, entry.Color.B), render.RGB{150, 150, 150}, rowBg)

		// Hex
		drawText(60, y, fmt.Sprintf("#%02X%02X%02X", entry.Color.R, entry.Color.G, entry.Color.B), render.RGB{150, 150, 150}, rowBg)

		// 256 index
		drawText(69, y, fmt.Sprintf("%3d", idx256), render.RGB{130, 130, 130}, rowBg)

		// Delta indicator (small visual if significant difference)
		delta := absDelta(entry.Color, rgb256)
		if delta > 50 {
			drawText(77, y, "!", render.RGB{255, 100, 100}, rowBg)
		} else if delta > 25 {
			drawText(77, y, "~", render.RGB{255, 200, 100}, rowBg)
		}
	}

	// Scroll indicator
	if len(gamePalette) > listHeight {
		scrollInfo := fmt.Sprintf("[%d/%d]", state.paletteIdx+1, len(gamePalette))
		drawText(state.width-len(scrollInfo)-2, startY-2, scrollInfo, render.RGB{100, 100, 100}, bg)
	}

	// Detailed info for selected color
	infoY := startY + listHeight + 1
	drawBox(0, infoY, 80, 8, " Selected Color Analysis ", render.RGB{80, 80, 80}, bg)

	if state.paletteIdx < len(gamePalette) {
		entry := gamePalette[state.paletteIdx]
		info := AnalyzeColor(entry.Color)
		drawColorInfo(2, infoY+1, info)

		// LUT match indicator
		lutNote := ""
		if info.Redmean256Bg == info.RGB {
			lutNote = "✓ Exact LUT match"
		} else {
			lutNote = fmt.Sprintf("≈ Approximated (Δ=%d)", absDelta(info.RGB, info.Redmean256Bg))
		}
		drawText(2, infoY+5, lutNote, render.RGB{180, 180, 100}, bg)
	}
}

func absDelta(a, b render.RGB) int {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	if dr < 0 {
		dr = -dr
	}
	if dg < 0 {
		dg = -dg
	}
	if db < 0 {
		db = -db
	}
	return dr + dg + db
}