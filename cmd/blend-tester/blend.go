package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

func handleBlendInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp:
		state.blendSrcIdx--
		if state.blendSrcIdx < 0 {
			state.blendSrcIdx = len(gamePalette) - 1
		}
	case terminal.KeyDown:
		state.blendSrcIdx++
		if state.blendSrcIdx >= len(gamePalette) {
			state.blendSrcIdx = 0
		}
	case terminal.KeyLeft:
		if ev.Modifiers&terminal.ModShift != 0 {
			state.blendAlpha -= 0.1
		} else {
			state.blendAlpha -= 0.01
		}
		if state.blendAlpha < 0 {
			state.blendAlpha = 0
		}
	case terminal.KeyRight:
		if ev.Modifiers&terminal.ModShift != 0 {
			state.blendAlpha += 0.1
		} else {
			state.blendAlpha += 0.01
		}
		if state.blendAlpha > 1 {
			state.blendAlpha = 1
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 's', 'S':
			state.blendSrcIdx = (state.blendSrcIdx + 1) % len(gamePalette)
		case 'd', 'D':
			state.blendDstIdx = (state.blendDstIdx + 1) % len(gamePalette)
		case 'o', 'O':
			state.blendOp = (state.blendOp + 1) % len(blendOps)
		case 'b', 'B':
			state.blendBgIdx = (state.blendBgIdx + 1) % len(bgPresets)
		case 'h', 'H':
			// Enter hex input for custom bg
			if state.blendBgIdx == 3 {
				state.hexInputActive = true
				state.hexInputTarget = 0
				state.hexInputBuffer = ""
			}
		case 'r', 'R':
			state.blendAlpha = 1.0
		}
	}
}

func drawBlendMode() {
	startY := 2
	fg := render.RGB{180, 180, 180}
	bg := render.RGB{20, 20, 30}

	// Keys help
	drawText(1, startY, "S:Src D:Dst O:Op ←→:Alpha(±0.01) Shift:±0.1 B:Bg H:HexInput R:Reset", render.RGB{100, 100, 100}, bg)
	startY += 2

	srcColor := gamePalette[state.blendSrcIdx].Color
	dstColor := gamePalette[state.blendDstIdx].Color
	op := blendOps[state.blendOp]

	bgColor := bgPresets[state.blendBgIdx].color
	if state.blendBgIdx == 3 {
		bgColor = state.blendCustomBg
	}

	// Source
	drawText(1, startY, fmt.Sprintf("SRC: %-18s", gamePalette[state.blendSrcIdx].Name), fg, bg)
	drawSwatch(25, startY, 5, srcColor)
	drawText(31, startY, fmt.Sprintf("(%3d,%3d,%3d)", srcColor.R, srcColor.G, srcColor.B), render.RGB{150, 150, 150}, bg)
	startY++

	// Destination
	drawText(1, startY, fmt.Sprintf("DST: %-18s", gamePalette[state.blendDstIdx].Name), fg, bg)
	drawSwatch(25, startY, 5, dstColor)
	drawText(31, startY, fmt.Sprintf("(%3d,%3d,%3d)", dstColor.R, dstColor.G, dstColor.B), render.RGB{150, 150, 150}, bg)
	startY++

	// Operation
	drawText(1, startY, fmt.Sprintf("OP:  %-12s", op.name), render.RGB{255, 200, 100}, bg)
	drawText(20, startY, fmt.Sprintf("α: %.2f", state.blendAlpha), render.RGB{100, 200, 255}, bg)
	startY++

	// Background
	bgName := bgPresets[state.blendBgIdx].name
	if state.blendBgIdx == 3 {
		bgName = fmt.Sprintf("Custom #%02X%02X%02X", state.blendCustomBg.R, state.blendCustomBg.G, state.blendCustomBg.B)
	}
	drawText(1, startY, fmt.Sprintf("BG:  %s", bgName), fg, bg)
	drawSwatch(25, startY, 5, bgColor)
	startY += 2

	// Formula
	drawBox(0, startY, 80, 3, " Formula ", render.RGB{80, 80, 80}, bg)
	drawText(2, startY+1, op.formula, render.RGB{200, 200, 100}, bg)
	startY += 4

	// Compute result using actual render functions
	var result render.RGB
	switch state.blendOp {
	case 0: // Replace
		result = srcColor
	case 1: // Alpha
		result = render.Blend(dstColor, srcColor, state.blendAlpha)
	case 2: // SetComponent
		result = render.Add(dstColor, srcColor, state.blendAlpha)
	case 3: // Max
		result = render.Max(dstColor, srcColor, state.blendAlpha)
	case 4: // SoftLight
		result = render.SoftLight(dstColor, srcColor, state.blendAlpha)
	case 5: // Screen
		result = render.Screen(dstColor, srcColor, state.blendAlpha)
	case 6: // Overlay
		result = render.Overlay(dstColor, srcColor, state.blendAlpha)
	}

	// Results box
	drawBox(0, startY, 80, 10, " Result ", render.RGB{80, 80, 80}, bg)
	startY++

	// TrueColor result
	drawText(2, startY, "TrueColor:", fg, bg)
	drawSwatch(15, startY, 8, result)
	drawText(24, startY, fmt.Sprintf("(%3d,%3d,%3d) #%02X%02X%02X", result.R, result.G, result.B, result.R, result.G, result.B), render.RGB{150, 150, 150}, bg)
	startY++

	// 256 result
	info := AnalyzeColor(result)
	drawText(2, startY, "256 Redmean:", fg, bg)
	drawSwatch(15, startY, 8, info.Redmean256Bg)
	drawText(24, startY, fmt.Sprintf("idx=%3d → (%3d,%3d,%3d)", info.Redmean256, info.Redmean256Bg.R, info.Redmean256Bg.G, info.Redmean256Bg.B), render.RGB{150, 150, 150}, bg)
	startY++

	drawText(2, startY, "256 Naive:", fg, bg)
	drawSwatch(15, startY, 8, info.Naive256RGB)
	drawText(24, startY, fmt.Sprintf("idx=%3d → (%3d,%3d,%3d)", info.Naive256, info.Naive256RGB.R, info.Naive256RGB.G, info.Naive256RGB.B), render.RGB{150, 150, 150}, bg)
	startY += 2

	// Side-by-side preview on background
	drawText(2, startY, "Preview on BG:", fg, bg)
	startY++
	// TC preview
	for x := 0; x < 20; x++ {
		buf.SetWithBg(2+x, startY, ' ', result, bgColor)
		buf.SetWithBg(2+x, startY+1, ' ', result, bgColor)
	}
	drawText(2, startY, "  TC  ", render.RGB{0, 0, 0}, result)
	// 256 preview
	for x := 0; x < 20; x++ {
		buf.SetWithBg(25+x, startY, ' ', info.Redmean256Bg, bgColor)
		buf.SetWithBg(25+x, startY+1, ' ', info.Redmean256Bg, bgColor)
	}
	drawText(25, startY, "  256 ", render.RGB{0, 0, 0}, info.Redmean256Bg)

	// Hex input overlay
	if state.hexInputActive && state.hexInputTarget == 0 {
		drawBox(20, 10, 30, 5, " Hex Input ", render.RGB{255, 255, 0}, render.RGB{40, 40, 60})
		drawText(22, 12, "#"+state.hexInputBuffer+"_", render.RGB{255, 255, 255}, render.RGB{40, 40, 60})
		drawText(22, 13, "Enter:Apply Esc:Cancel", render.RGB{100, 100, 100}, render.RGB{40, 40, 60})
	}
}