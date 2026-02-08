package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// 256-color palette cube values
var cubeValues = [6]int{0, 95, 135, 175, 215, 255}

// Cube midpoints for naive mapping
var cubeMidpoints = [6]int{0, 47, 115, 155, 195, 235}

// NaiveCube256 does simple cube snapping without Redmean weighting
func NaiveCube256(c terminal.RGB) uint8 {
	// Grayscale check
	if c.R == c.G && c.G == c.B {
		if c.R < 8 {
			return 16
		}
		if c.R > 238 {
			return 231
		}
		return uint8(232 + (int(c.R)-8)/10)
	}

	// Find nearest cube indices using midpoints
	ri := snapToCube(int(c.R))
	gi := snapToCube(int(c.G))
	bi := snapToCube(int(c.B))

	return uint8(16 + ri*36 + gi*6 + bi)
}

func snapToCube(v int) int {
	for i := 0; i < 5; i++ {
		if v < cubeMidpoints[i+1] {
			return i
		}
	}
	return 5
}

// Get256PaletteRGB returns the actual RGB for a 256 palette index
func Get256PaletteRGB(idx uint8) terminal.RGB {
	if idx < 16 {
		// System colors - approximate
		return systemColors[idx]
	}
	if idx < 232 {
		// 6x6x6 cube
		i := int(idx) - 16
		ri := i / 36
		gi := (i / 6) % 6
		bi := i % 6
		return terminal.RGB{
			R: uint8(cubeValues[ri]),
			G: uint8(cubeValues[gi]),
			B: uint8(cubeValues[bi]),
		}
	}
	// Grayscale ramp 232-255
	gray := uint8(8 + (int(idx)-232)*10)
	return terminal.RGB{R: gray, G: gray, B: gray}
}

var systemColors = [16]terminal.RGB{
	{0, 0, 0},       // 0 black
	{128, 0, 0},     // 1 red
	{0, 128, 0},     // 2 green
	{128, 128, 0},   // 3 yellow
	{0, 0, 128},     // 4 blue
	{128, 0, 128},   // 5 magenta
	{0, 128, 128},   // 6 cyan
	{192, 192, 192}, // 7 white
	{128, 128, 128}, // 8 bright black
	{255, 0, 0},     // 9 bright red
	{0, 255, 0},     // 10 bright green
	{255, 255, 0},   // 11 bright yellow
	{0, 0, 255},     // 12 bright blue
	{255, 0, 255},   // 13 bright magenta
	{0, 255, 255},   // 14 bright cyan
	{255, 255, 255}, // 15 bright white
}

// ColorInfo holds complete color analysis
type ColorInfo struct {
	RGB          terminal.RGB
	Hex          string
	Redmean256   uint8
	Redmean256Bg terminal.RGB
	Naive256     uint8
	Naive256RGB  terminal.RGB
	DeltaR       int
	DeltaG       int
	DeltaB       int
}

func AnalyzeColor(c terminal.RGB) ColorInfo {
	redmeanIdx := terminal.RGBTo256(c)
	naiveIdx := NaiveCube256(c)
	redmeanRGB := Get256PaletteRGB(redmeanIdx)
	naiveRGB := Get256PaletteRGB(naiveIdx)

	return ColorInfo{
		RGB:          c,
		Hex:          fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B),
		Redmean256:   redmeanIdx,
		Redmean256Bg: redmeanRGB,
		Naive256:     naiveIdx,
		Naive256RGB:  naiveRGB,
		DeltaR:       int(c.R) - int(redmeanRGB.R),
		DeltaG:       int(c.G) - int(redmeanRGB.G),
		DeltaB:       int(c.B) - int(redmeanRGB.B),
	}
}

// Drawing helpers

func drawText(x, y int, text string, fg, bg terminal.RGB) {
	for i, ch := range text {
		if x+i >= 0 && x+i < state.width && y >= 0 && y < state.height {
			buf.SetWithBg(x+i, y, ch, fg, bg)
		}
	}
}

func drawTextFg(x, y int, text string, fg terminal.RGB) {
	for i, ch := range text {
		if x+i >= 0 && x+i < state.width && y >= 0 && y < state.height {
			buf.SetFgOnly(x+i, y, ch, fg, terminal.AttrNone)
		}
	}
}

func drawSwatch(x, y, w int, c terminal.RGB) {
	for i := 0; i < w; i++ {
		if x+i >= 0 && x+i < state.width && y >= 0 && y < state.height {
			buf.SetWithBg(x+i, y, ' ', c, c)
		}
	}
}

func drawSwatchChar(x, y int, ch rune, fg, bg terminal.RGB) {
	if x >= 0 && x < state.width && y >= 0 && y < state.height {
		buf.SetWithBg(x, y, ch, fg, bg)
	}
}

func drawBox(x, y, w, h int, title string, fg, bg terminal.RGB) {
	// Top border
	drawSwatchChar(x, y, '┌', fg, bg)
	for i := 1; i < w-1; i++ {
		drawSwatchChar(x+i, y, '─', fg, bg)
	}
	drawSwatchChar(x+w-1, y, '┐', fg, bg)

	// Title
	if title != "" && len(title)+2 < w-2 {
		tx := x + (w-len(title))/2
		drawText(tx, y, title, terminal.RGB{255, 255, 0}, bg)
	}

	// Sides
	for j := 1; j < h-1; j++ {
		drawSwatchChar(x, y+j, '│', fg, bg)
		for i := 1; i < w-1; i++ {
			drawSwatchChar(x+i, y+j, ' ', fg, bg)
		}
		drawSwatchChar(x+w-1, y+j, '│', fg, bg)
	}

	// Bottom border
	drawSwatchChar(x, y+h-1, '└', fg, bg)
	for i := 1; i < w-1; i++ {
		drawSwatchChar(x+i, y+h-1, '─', fg, bg)
	}
	drawSwatchChar(x+w-1, y+h-1, '┘', fg, bg)
}

// DrawColorInfoCompact draws a compact single-line color summary
func DrawColorInfoCompact(x, y int, c terminal.RGB, label string) {
	fg := terminal.RGB{180, 180, 180}
	bg := terminal.RGB{20, 20, 30}

	drawText(x, y, label+":", fg, bg)
	labelLen := len(label) + 2
	drawSwatch(x+labelLen, y, 3, c)
	drawText(x+labelLen+4, y, fmt.Sprintf("(%3d,%3d,%3d) #%02X%02X%02X", c.R, c.G, c.B, c.R, c.G, c.B), terminal.RGB{150, 150, 150}, bg)
}

func drawColorInfo(x, y int, info ColorInfo) int {
	fg := terminal.RGB{200, 200, 200}
	bg := terminal.RGB{20, 20, 30}

	line := y
	drawText(x, line, fmt.Sprintf("RGB: (%3d,%3d,%3d)  Hex: %s", info.RGB.R, info.RGB.G, info.RGB.B, info.Hex), fg, bg)
	drawSwatch(x+38, line, 3, info.RGB)
	line++

	drawText(x, line, fmt.Sprintf("Redmean 256: idx=%3d → (%3d,%3d,%3d)", info.Redmean256, info.Redmean256Bg.R, info.Redmean256Bg.G, info.Redmean256Bg.B), fg, bg)
	drawSwatch(x+38, line, 3, info.Redmean256Bg)
	line++

	drawText(x, line, fmt.Sprintf("Naive 256:   idx=%3d → (%3d,%3d,%3d)", info.Naive256, info.Naive256RGB.R, info.Naive256RGB.G, info.Naive256RGB.B), fg, bg)
	drawSwatch(x+38, line, 3, info.Naive256RGB)
	line++

	drawText(x, line, fmt.Sprintf("Delta (Redmean): R%+4d G%+4d B%+4d", info.DeltaR, info.DeltaG, info.DeltaB), terminal.RGB{150, 150, 150}, bg)
	line++

	return line
}