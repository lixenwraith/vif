package main

import (
	"fmt"
	"math"

	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

func handleEffectInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyRune:
		switch ev.Rune {
		case '1':
			state.effectSub = EffectShield
		case '2':
			state.effectSub = EffectTrail
		case '3':
			state.effectSub = EffectFlash
		case '4':
			state.effectSub = EffectHeat
		}
	}

	// Sub-mode specific
	switch state.effectSub {
	case EffectShield:
		handleShieldInput(ev)
	case EffectTrail:
		handleTrailInput(ev)
	case EffectFlash:
		handleFlashInput(ev)
	case EffectHeat:
		handleHeatInput(ev)
	}
}

func handleShieldInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp:
		if state.shieldRadiusY < 10.0 {
			state.shieldRadiusY += 0.5
		}
	case terminal.KeyDown:
		if state.shieldRadiusY > 1.0 {
			state.shieldRadiusY -= 0.5
		}
	case terminal.KeyLeft:
		if state.shieldRadiusX > 1.0 {
			state.shieldRadiusX -= 0.5
		}
	case terminal.KeyRight:
		if state.shieldRadiusX < 15.0 {
			state.shieldRadiusX += 0.5
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 'o', 'O':
			state.shieldOpacity -= 0.05
			if state.shieldOpacity < 0.1 {
				state.shieldOpacity = 0.1
			}
		case 'p', 'P':
			state.shieldOpacity += 0.05
			if state.shieldOpacity > 1.0 {
				state.shieldOpacity = 1.0
			}
		case 'c', 'C':
			// Cycle: Gray → Blue → Green → Custom → Gray
			if state.shieldColorMode == 1 {
				state.shieldColorMode = 0
				state.shieldState = ShieldGray
			} else {
				state.shieldState++
				if state.shieldState > ShieldGreen {
					state.shieldColorMode = 1 // Switch to custom
				}
			}
		case 'b', 'B':
			state.shieldBgIdx = (state.shieldBgIdx + 1) % len(bgPresets)
		case 'h', 'H':
			// Hex input for custom shield color
			state.hexInputActive = true
			state.hexInputBuffer = ""
			state.hexInputTarget = 2 // New target: shield custom color
		}
	}
}

func handleTrailInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp:
		if state.trailLength < 20 {
			state.trailLength++
		}
	case terminal.KeyDown:
		if state.trailLength > 1 {
			state.trailLength--
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 'c', 'C':
			state.trailColorIdx = (state.trailColorIdx + 1) % len(gamePalette)
		case 't', 'T':
			state.trailType = (state.trailType + 1) % 2
		}
	}
}

func handleFlashInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp:
		if state.flashFrame < state.flashDuration-1 {
			state.flashFrame++
		}
	case terminal.KeyDown:
		if state.flashFrame > 0 {
			state.flashFrame--
		}
	case terminal.KeyLeft:
		if state.flashDuration > 1 {
			state.flashDuration--
			if state.flashFrame >= state.flashDuration {
				state.flashFrame = state.flashDuration - 1
			}
		}
	case terminal.KeyRight:
		if state.flashDuration < 30 {
			state.flashDuration++
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 'c', 'C':
			state.flashColorIdx = (state.flashColorIdx + 1) % len(gamePalette)
		}
	}
}

func handleHeatInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyUp, terminal.KeyRight:
		if state.heatValue < 100 {
			state.heatValue += 5
		}
	case terminal.KeyDown, terminal.KeyLeft:
		if state.heatValue > 0 {
			state.heatValue -= 5
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 'b', 'B':
			state.heatBgIdx = (state.heatBgIdx + 1) % len(bgPresets)
		}
	}
}

func drawEffectMode() {
	startY := 2
	fg := terminal.RGB{180, 180, 180}
	bg := terminal.RGB{20, 20, 30}

	// Sub-mode tabs
	subModes := []string{"1:Shield", "2:Trail", "3:Flash", "4:Heat"}
	x := 1
	for i, m := range subModes {
		tabFg := terminal.RGB{150, 150, 150}
		tabBg := terminal.RGB{40, 40, 40}
		if EffectSubMode(i) == state.effectSub {
			tabFg = terminal.RGB{0, 0, 0}
			tabBg = terminal.RGB{0, 200, 200}
		}
		drawText(x, startY, " "+m+" ", tabFg, tabBg)
		x += len(m) + 3
	}
	startY += 2

	switch state.effectSub {
	case EffectShield:
		drawShieldEffect(startY, fg, bg)
	case EffectTrail:
		drawTrailEffect(startY, fg, bg)
	case EffectFlash:
		drawFlashEffect(startY, fg, bg)
	case EffectHeat:
		drawHeatEffect(startY, fg, bg)
	}
}

func drawShieldEffect(startY int, fg, bg terminal.RGB) {
	// Keys line - updated
	drawText(1, startY, "←→:RadX ↑↓:RadY O/P:Opacity C:Color B:Bg H:HexColor", terminal.RGB{100, 100, 100}, bg)
	startY += 2

	// Parameters - updated to show color mode
	var colorLabel string
	if state.shieldColorMode == 1 {
		colorLabel = fmt.Sprintf("Custom #%02X%02X%02X", state.shieldCustomColor.R, state.shieldCustomColor.G, state.shieldCustomColor.B)
	} else {
		stateNames := []string{"Gray (No Char)", "Blue (Dark/Normal/Bright)", "Green (Dark/Normal/Bright)"}
		colorLabel = stateNames[state.shieldState]
	}
	drawText(1, startY, fmt.Sprintf("RadiusX: %.1f  RadiusY: %.1f  Opacity: %.2f", state.shieldRadiusX, state.shieldRadiusY, state.shieldOpacity), fg, bg)
	startY++
	drawText(1, startY, fmt.Sprintf("Color: %s", colorLabel), fg, bg)

	// Color swatch for custom mode
	if state.shieldColorMode == 1 {
		drawSwatch(8+len(colorLabel), startY, 4, state.shieldCustomColor)
	}
	startY++

	bgName := bgPresets[state.shieldBgIdx].name
	bgColor := bgPresets[state.shieldBgIdx].color
	drawText(1, startY, fmt.Sprintf("Background: %s", bgName), fg, bg)
	startY += 2

	// Determine shield color
	var shieldColor terminal.RGB
	if state.shieldColorMode == 1 {
		shieldColor = state.shieldCustomColor
	} else {
		switch state.shieldState {
		case ShieldGray:
			shieldColor = terminal.RGB{128, 128, 128}
		case ShieldBlue:
			shieldColor = visual.RgbGlyphBlueNormal
		case ShieldGreen:
			shieldColor = visual.RgbGlyphGreenNormal
		}
	}

	// Preview area - side by side TC and 256
	previewW := int(state.shieldRadiusX)*2 + 3
	previewH := int(state.shieldRadiusY)*2 + 3
	if previewW > 40 {
		previewW = 40
	}
	if previewH > 15 {
		previewH = 15
	}

	centerX := previewW / 2
	centerY := previewH / 2

	// TC Preview
	tcStartX := 2
	drawText(tcStartX, startY, "TrueColor:", fg, bg)
	startY++
	drawShieldPreview(tcStartX, startY, previewW, previewH, centerX, centerY, shieldColor, bgColor, true)

	// 256 Preview
	x256StartX := tcStartX + previewW + 5
	drawText(x256StartX, startY-1, "256-Color:", fg, bg)
	drawShieldPreview(x256StartX, startY, previewW, previewH, centerX, centerY, shieldColor, bgColor, false)

	// Formula
	startY += previewH + 2
	drawBox(0, startY, 70, 5, " Shield Blend (Screen) ", terminal.RGB{80, 80, 80}, bg)
	drawText(2, startY+1, "falloff = (1 - dist)²", terminal.RGB{200, 200, 100}, bg)
	drawText(2, startY+2, "alpha = falloff × maxOpacity", terminal.RGB{200, 200, 100}, bg)
	drawText(2, startY+3, "Screen: 255 - (255-Dst)*(255-Src)/255", terminal.RGB{200, 200, 100}, bg)

	// Hex input overlay for shield
	if state.hexInputActive && state.hexInputTarget == 2 {
		drawBox(20, 10, 30, 5, " Shield Color ", terminal.RGB{255, 255, 0}, terminal.RGB{40, 40, 60})
		drawText(22, 12, "#"+state.hexInputBuffer+"_", terminal.RGB{255, 255, 255}, terminal.RGB{40, 40, 60})
		drawText(22, 13, "Enter:Apply Esc:Cancel", terminal.RGB{100, 100, 100}, terminal.RGB{40, 40, 60})
	}
}

func drawShieldPreview(startX, startY, w, h, cx, cy int, shieldColor, bgColor terminal.RGB, trueColor bool) {
	invRxSq := 1.0 / (state.shieldRadiusX * state.shieldRadiusX)
	invRySq := 1.0 / (state.shieldRadiusY * state.shieldRadiusY)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			screenX := startX + x
			screenY := startY + y

			dx := float64(x - cx)
			dy := float64(y - cy)

			normalizedDistSq := dx*dx*invRxSq + dy*dy*invRySq

			if normalizedDistSq > 1.0 {
				// Outside shield
				buf.SetWithBg(screenX, screenY, '·', terminal.RGB{60, 60, 60}, bgColor)
				continue
			}

			dist := math.Sqrt(normalizedDistSq)

			// Center marker
			if x == cx && y == cy {
				buf.SetWithBg(screenX, screenY, '+', terminal.RGB{255, 255, 255}, bgColor)
				continue
			}

			if trueColor {
				// TrueColor: smooth gradient
				falloff := (1.0 - dist) * (1.0 - dist)
				alpha := falloff * state.shieldOpacity
				blended := render.Screen(bgColor, shieldColor, alpha)
				buf.SetWithBg(screenX, screenY, ' ', blended, blended)
			} else {
				// 256: rim-only mode (matching cell256 in shields.go)
				if dist < 0.6 {
					buf.SetWithBg(screenX, screenY, ' ', bgColor, bgColor)
				} else {
					blended := render.Screen(bgColor, shieldColor, 0.6)
					idx := terminal.RGBTo256(blended)
					rgb256 := Get256PaletteRGB(idx)
					buf.SetWithBg(screenX, screenY, ' ', rgb256, rgb256)
				}
			}
		}
	}
}

func drawTrailEffect(startY int, fg, bg terminal.RGB) {
	drawText(1, startY, "↑↓:Length C:Color T:Type(Cleaner/Materialize)", terminal.RGB{100, 100, 100}, bg)
	startY += 2

	typeName := "Cleaner"
	baseColor := visual.RgbCleanerBasePositive
	if state.trailType == 1 {
		typeName = "Materialize"
		baseColor = visual.RgbMaterialize
	}
	if state.trailColorIdx > 0 && state.trailColorIdx < len(gamePalette) {
		baseColor = gamePalette[state.trailColorIdx].Color
	}

	drawText(1, startY, fmt.Sprintf("Type: %s  Length: %d  BaseColor:", typeName, state.trailLength), fg, bg)
	drawSwatch(45, startY, 4, baseColor)
	startY += 2

	// Draw gradient strip
	drawText(1, startY, "Gradient (head→tail):", fg, bg)
	startY++

	// TC strip
	drawText(1, startY, "TC:", fg, bg)
	for i := 0; i < state.trailLength; i++ {
		opacity := 1.0 - (float64(i) / float64(state.trailLength))
		if opacity < 0 {
			opacity = 0
		}
		scaled := render.Scale(baseColor, opacity)
		buf.SetWithBg(5+i*2, startY, '█', scaled, bg)
		buf.SetWithBg(6+i*2, startY, '█', scaled, bg)
	}
	startY++

	// 256 strip
	drawText(1, startY, "256:", fg, bg)
	for i := 0; i < state.trailLength; i++ {
		opacity := 1.0 - (float64(i) / float64(state.trailLength))
		if opacity < 0 {
			opacity = 0
		}
		scaled := render.Scale(baseColor, opacity)
		idx := terminal.RGBTo256(scaled)
		rgb256 := Get256PaletteRGB(idx)
		buf.SetWithBg(5+i*2, startY, '█', rgb256, bg)
		buf.SetWithBg(6+i*2, startY, '█', rgb256, bg)
	}
	startY += 2

	// Formula
	drawBox(0, startY, 50, 3, " Trail Formula ", terminal.RGB{80, 80, 80}, bg)
	drawText(2, startY+1, "opacity = 1.0 - (idx / length)", terminal.RGB{200, 200, 100}, bg)
}

func drawFlashEffect(startY int, fg, bg terminal.RGB) {
	drawText(1, startY, "↑↓:Frame ←→:Duration C:Color", terminal.RGB{100, 100, 100}, bg)
	startY += 2

	baseColor := visual.RgbRemovalFlash
	if state.flashColorIdx > 0 && state.flashColorIdx < len(gamePalette) {
		baseColor = gamePalette[state.flashColorIdx].Color
	}

	drawText(1, startY, fmt.Sprintf("Frame: %d/%d  BaseColor:", state.flashFrame+1, state.flashDuration), fg, bg)
	drawSwatch(30, startY, 4, baseColor)
	startY += 2

	// Calculate opacity for current frame
	opacity := 1.0 - (float64(state.flashFrame) / float64(state.flashDuration))
	if opacity < 0 {
		opacity = 0
	}

	// Flash uses AddEntityAt blend on foreground
	flashColor := terminal.RGB{
		R: uint8(float64(baseColor.R) * opacity),
		G: uint8(float64(baseColor.G) * opacity),
		B: uint8(float64(baseColor.B) * opacity),
	}

	drawText(1, startY, fmt.Sprintf("Opacity: %.2f", opacity), fg, bg)
	startY++
	drawText(1, startY, "Flash contribution:", fg, bg)
	drawSwatch(22, startY, 6, flashColor)
	drawText(29, startY, fmt.Sprintf("(%3d,%3d,%3d)", flashColor.R, flashColor.G, flashColor.B), terminal.RGB{150, 150, 150}, bg)
	startY += 2

	// Show add blend result on sample char
	drawText(1, startY, "AddEntityAt blend on char 'A' (green):", fg, bg)
	startY++
	charFg := visual.RgbGlyphGreenNormal
	addedTC := render.Add(charFg, flashColor, 1.0)
	idx := terminal.RGBTo256(addedTC)
	added256 := Get256PaletteRGB(idx)

	drawText(1, startY, "TC:", fg, bg)
	buf.SetWithBg(5, startY, 'A', addedTC, bg)
	drawText(7, startY, fmt.Sprintf("(%3d,%3d,%3d)", addedTC.R, addedTC.G, addedTC.B), terminal.RGB{150, 150, 150}, bg)

	drawText(30, startY, "256:", fg, bg)
	buf.SetWithBg(35, startY, 'A', added256, bg)
	drawText(37, startY, fmt.Sprintf("idx=%3d", idx), terminal.RGB{150, 150, 150}, bg)
	startY += 2

	// Formula
	drawBox(0, startY, 55, 4, " Flash Formula (BlendAddFg) ", terminal.RGB{80, 80, 80}, bg)
	drawText(2, startY+1, "opacity = 1.0 - (elapsed / duration)", terminal.RGB{200, 200, 100}, bg)
	drawText(2, startY+2, "result = min(charFg + flashColor*opacity, 255)", terminal.RGB{200, 200, 100}, bg)
}

func drawHeatEffect(startY int, fg, bg terminal.RGB) {
	// Updated keys line
	drawText(1, startY, "←→ ↑↓:Value B:Background", terminal.RGB{100, 100, 100}, bg)
	startY += 2

	// Show background selection
	bgName := bgPresets[state.heatBgIdx].name
	bgColor := bgPresets[state.heatBgIdx].color
	drawText(1, startY, fmt.Sprintf("Heat Value: %d%%  Background: %s", state.heatValue, bgName), fg, bg)
	startY += 2

	// Full gradient display
	drawText(1, startY, "Heat Meter Gradient (0→100%):", fg, bg)
	startY++

	gradientW := 60
	// TC gradient
	drawText(1, startY, "TC:", fg, bgColor)
	for i := 0; i < gradientW; i++ {
		progress := float64(i+1) / float64(gradientW)
		color := getHeatColor(progress)
		buf.SetWithBg(5+i, startY, ' ', color, color)
	}
	startY++

	// 256 gradient
	drawText(1, startY, "256:", fg, bgColor)
	for i := 0; i < gradientW; i++ {
		progress := float64(i+1) / float64(gradientW)
		color := getHeatColor(progress)
		idx := terminal.RGBTo256(color)
		rgb256 := Get256PaletteRGB(idx)
		buf.SetWithBg(5+i, startY, ' ', rgb256, rgb256)
	}
	startY += 2

	// Current value indicator
	progress := float64(state.heatValue) / 100.0
	currentColor := getHeatColor(progress)
	drawText(1, startY, fmt.Sprintf("Current (%d%%):", state.heatValue), fg, bgColor)
	drawSwatch(18, startY, 6, currentColor)

	info := AnalyzeColor(currentColor)
	drawText(25, startY, fmt.Sprintf("TC: (%3d,%3d,%3d)", currentColor.R, currentColor.G, currentColor.B), terminal.RGB{150, 150, 150}, bgColor)
	startY++
	drawText(25, startY, fmt.Sprintf("256: idx=%3d → (%3d,%3d,%3d)", info.Redmean256, info.Redmean256Bg.R, info.Redmean256Bg.G, info.Redmean256Bg.B), terminal.RGB{150, 150, 150}, bgColor)
	startY += 2

	// Segment markers
	drawText(1, startY, "Segments: 0  10  20  30  40  50  60  70  80  90 100", terminal.RGB{120, 120, 120}, bgColor)
}

// getHeatColor retrieves gradient color from LUT, mapping progress to index
func getHeatColor(progress float64) terminal.RGB {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	idx := int(progress * 255)
	return render.HeatGradientLUT[idx]
}