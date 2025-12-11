// @focus: #render { color }
package main

import (
	"fmt"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

func handleDiagInput(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyRune:
		if ev.Rune == 'h' || ev.Rune == 'H' {
			state.hexInputActive = true
			state.hexInputTarget = 1
			state.hexInputBuffer = ""
		}
	case terminal.KeyUp:
		// Increment R
		if state.diagInputRGB.R < 255 {
			state.diagInputRGB.R++
			updateDiagHex()
		}
	case terminal.KeyDown:
		// Decrement R
		if state.diagInputRGB.R > 0 {
			state.diagInputRGB.R--
			updateDiagHex()
		}
	case terminal.KeyRight:
		// Increment G
		if state.diagInputRGB.G < 255 {
			state.diagInputRGB.G++
			updateDiagHex()
		}
	case terminal.KeyLeft:
		// Decrement G
		if state.diagInputRGB.G > 0 {
			state.diagInputRGB.G--
			updateDiagHex()
		}
	case terminal.KeyPageUp:
		// Increment B
		if state.diagInputRGB.B < 255 {
			state.diagInputRGB.B++
			updateDiagHex()
		}
	case terminal.KeyPageDown:
		// Decrement B
		if state.diagInputRGB.B > 0 {
			state.diagInputRGB.B--
			updateDiagHex()
		}
	}
}

func updateDiagHex() {
	state.diagInputHex = fmt.Sprintf("%02X%02X%02X", state.diagInputRGB.R, state.diagInputRGB.G, state.diagInputRGB.B)
}

func drawDiagMode() {
	startY := 2
	fg := render.RGB{180, 180, 180}
	bg := render.RGB{20, 20, 30}
	dimFg := render.RGB{120, 120, 120}

	drawText(1, startY, "H:Hex input  ↑↓:R±1  ←→:G±1  PgUp/Dn:B±1", render.RGB{100, 100, 100}, bg)
	startY += 2

	// Current color display
	drawBox(0, startY, 78, 7, " Color Analysis ", render.RGB{80, 80, 80}, bg)

	drawText(2, startY+1, fmt.Sprintf("Input: #%s  RGB: (%3d, %3d, %3d)",
		state.diagInputHex, state.diagInputRGB.R, state.diagInputRGB.G, state.diagInputRGB.B), fg, bg)

	info := AnalyzeColor(state.diagInputRGB)

	// Three-way comparison
	drawText(2, startY+3, "TrueColor:", dimFg, bg)
	drawSwatch(14, startY+3, 8, info.RGB)
	drawText(23, startY+3, fmt.Sprintf("(%3d,%3d,%3d)", info.RGB.R, info.RGB.G, info.RGB.B), dimFg, bg)

	drawText(2, startY+4, "Redmean:", dimFg, bg)
	drawSwatch(14, startY+4, 8, info.Redmean256Bg)
	drawText(23, startY+4, fmt.Sprintf("idx=%3d → (%3d,%3d,%3d)", info.Redmean256, info.Redmean256Bg.R, info.Redmean256Bg.G, info.Redmean256Bg.B), dimFg, bg)

	drawText(2, startY+5, "Naive:", dimFg, bg)
	drawSwatch(14, startY+5, 8, info.Naive256RGB)
	drawText(23, startY+5, fmt.Sprintf("idx=%3d → (%3d,%3d,%3d)", info.Naive256, info.Naive256RGB.R, info.Naive256RGB.G, info.Naive256RGB.B), dimFg, bg)

	// Delta from TC
	redmeanDelta := absDelta(info.RGB, info.Redmean256Bg)
	naiveDelta := absDelta(info.RGB, info.Naive256RGB)
	drawText(55, startY+4, fmt.Sprintf("Δ=%3d", redmeanDelta), deltaColor(redmeanDelta), bg)
	drawText(55, startY+5, fmt.Sprintf("Δ=%3d", naiveDelta), deltaColor(naiveDelta), bg)

	// Winner indicator
	if redmeanDelta < naiveDelta {
		drawText(63, startY+4, "◀ closer", render.RGB{100, 255, 100}, bg)
	} else if naiveDelta < redmeanDelta {
		drawText(63, startY+5, "◀ closer", render.RGB{100, 255, 100}, bg)
	} else {
		drawText(63, startY+4, "= equal", render.RGB{200, 200, 100}, bg)
	}

	startY += 8

	// Quantization trace
	drawBox(0, startY, 78, 8, " Redmean LUT Trace ", render.RGB{80, 80, 80}, bg)
	r6 := state.diagInputRGB.R >> 2
	g6 := state.diagInputRGB.G >> 2
	b6 := state.diagInputRGB.B >> 2
	lutIdx := int(r6)<<12 | int(g6)<<6 | int(b6)

	drawText(2, startY+1, fmt.Sprintf("1. Input RGB:    (%3d, %3d, %3d)", state.diagInputRGB.R, state.diagInputRGB.G, state.diagInputRGB.B), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+2, fmt.Sprintf("2. Quantize >>2: (%3d, %3d, %3d)  [6-bit per channel]", r6, g6, b6), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+3, fmt.Sprintf("3. LUT index:    %d<<12 | %d<<6 | %d = %d", r6, g6, b6, lutIdx), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+4, fmt.Sprintf("4. LUT[%d] → palette index %d", lutIdx, info.Redmean256), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+5, fmt.Sprintf("5. Palette[%d] → RGB (%3d, %3d, %3d)", info.Redmean256, info.Redmean256Bg.R, info.Redmean256Bg.G, info.Redmean256Bg.B), render.RGB{200, 200, 100}, bg)
	startY += 9

	// Naive cube trace
	drawBox(0, startY, 78, 5, " Naive Cube Trace ", render.RGB{80, 80, 80}, bg)
	ri := snapToCube(int(state.diagInputRGB.R))
	gi := snapToCube(int(state.diagInputRGB.G))
	bi := snapToCube(int(state.diagInputRGB.B))
	drawText(2, startY+1, fmt.Sprintf("1. Snap R=%3d → cube[%d]=%3d", state.diagInputRGB.R, ri, cubeValues[ri]), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+2, fmt.Sprintf("2. Snap G=%3d → cube[%d]=%3d", state.diagInputRGB.G, gi, cubeValues[gi]), render.RGB{200, 200, 100}, bg)
	drawText(2, startY+3, fmt.Sprintf("3. Snap B=%3d → cube[%d]=%3d   idx = 16 + %d*36 + %d*6 + %d = %d",
		state.diagInputRGB.B, bi, cubeValues[bi], ri, gi, bi, info.Naive256), render.RGB{200, 200, 100}, bg)

	// Hex input overlay
	if state.hexInputActive && state.hexInputTarget == 1 {
		drawBox(20, 10, 30, 5, " Hex Input ", render.RGB{255, 255, 0}, render.RGB{40, 40, 60})
		drawText(22, 12, "#"+state.hexInputBuffer+"_", render.RGB{255, 255, 255}, render.RGB{40, 40, 60})
		drawText(22, 13, "Enter:Apply Esc:Cancel", render.RGB{100, 100, 100}, render.RGB{40, 40, 60})
	}
}

func deltaColor(delta int) render.RGB {
	if delta < 15 {
		return render.RGB{100, 255, 100} // Green - good
	} else if delta < 40 {
		return render.RGB{255, 255, 100} // Yellow - ok
	}
	return render.RGB{255, 100, 100} // Red - poor
}