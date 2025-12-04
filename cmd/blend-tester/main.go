package main

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Config holds the interactive state
type Config struct {
	RimThreshold float64
	Alpha        float64
	BlendMode    render.BlendMode
	ModeName     string
	BgColor      render.RGB
}

var (
	// All game colors from render/colors.go
	namedColors = []struct {
		name string
		c    render.RGB
	}{
		{"ShieldBase", render.RgbShieldBase},
		{"SeqGreenN", render.RgbSequenceGreenNormal},
		{"SeqRedN", render.RgbSequenceRedNormal},
		{"SeqBlueN", render.RgbSequenceBlueNormal},
		{"SeqGold", render.RgbSequenceGold},
		{"Decay", render.RgbDecay},
		{"Drain", render.RgbDrain},
		{"Nugget", render.RgbNuggetOrange},
		{"Cursor", render.RgbCursorNormal},
		{"Cleaner", render.RgbCleanerBase},
		{"Materialize", render.RgbMaterialize},
		{"Ping", render.RgbPingNormal},
	}

	// Blend modes to cycle through
	blendModes = []struct {
		name string
		mode render.BlendMode
	}{
		{"Screen", render.BlendScreen},
		{"Add", render.BlendAdd},
		{"Alpha", render.BlendAlpha},
		{"SoftLight", render.BlendSoftLight},
		{"Overlay", render.BlendOverlay},
		{"Max", render.BlendMax},
	}
)

func main() {
	// Force TrueColor for the tester itself so we can simulate 256 accurately on the screen
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	// Initial State
	state := Config{
		RimThreshold: 0.85, // Default from current shield code
		Alpha:        0.80, // Default from current shield code
		BlendMode:    render.BlendScreen,
		ModeName:     "Screen",
		BgColor:      render.RgbBackground,
	}

	w, h := term.Size()
	cells := make([]terminal.Cell, w*h)
	eventCh := make(chan terminal.Event, 10)

	// Input loop
	go func() {
		for {
			eventCh <- term.PollEvent()
		}
	}()

	modeIdx := 0

	// Main Loop
	for {
		// --- Logic ---
		select {
		case ev := <-eventCh:
			if ev.Type == terminal.EventKey {
				switch ev.Key {
				case terminal.KeyCtrlC, terminal.KeyEscape, terminal.KeyCtrlQ:
					return
				case terminal.KeyRight: // Increase Threshold
					state.RimThreshold += 0.05
					if state.RimThreshold > 1.0 {
						state.RimThreshold = 1.0
					}
				case terminal.KeyLeft: // Decrease Threshold
					state.RimThreshold -= 0.05
					if state.RimThreshold < 0.0 {
						state.RimThreshold = 0.0
					}
				case terminal.KeyUp: // Increase Alpha
					state.Alpha += 0.05
					if state.Alpha > 1.0 {
						state.Alpha = 1.0
					}
				case terminal.KeyDown: // Decrease Alpha
					state.Alpha -= 0.05
					if state.Alpha < 0.0 {
						state.Alpha = 0.0
					}
				case terminal.KeyTab: // Cycle Blend Mode Forward
					modeIdx = (modeIdx + 1) % len(blendModes)
					state.BlendMode = blendModes[modeIdx].mode
					state.ModeName = blendModes[modeIdx].name
				case terminal.KeyBacktab: // Cycle Blend Mode Backward (Shift+Tab)
					modeIdx = (modeIdx - 1 + len(blendModes)) % len(blendModes)
					state.BlendMode = blendModes[modeIdx].mode
					state.ModeName = blendModes[modeIdx].name
				}
			} else if ev.Type == terminal.EventResize {
				w, h = ev.Width, ev.Height
				cells = make([]terminal.Cell, w*h)
				term.Sync()
			}
		default:
		}

		// --- Rendering ---

		// Clear
		clearCells(cells, render.RGBBlack)

		// 1. Header Info
		printStr(cells, w, 0, 0, "VI-FIGHTER BLEND TESTER", render.RGB{255, 255, 255})
		printStr(cells, w, 0, 1, fmt.Sprintf("Mode [Tab/S-Tab]: %-10s  Alpha [Up/Dn]: %.2f  RimStart [Lt/Rt]: %.2f", state.ModeName, state.Alpha, state.RimThreshold), render.RgbNuggetOrange)

		// 2. Shield Gradient Simulation
		printStr(cells, w, 0, 4, "--- Shield Gradient (Simulated 256 behavior vs TC) ---", render.RgbLineNumbers)
		drawShieldStrip(cells, w, 5, state)

		// 3. Palette Matrix
		printStr(cells, w, 0, 10, "--- Global Palette Blend Test ---", render.RgbLineNumbers)
		drawPaletteGrid(cells, w, 11, state)

		term.Flush(cells, w, h)
		time.Sleep(16 * time.Millisecond)
	}
}

func drawShieldStrip(cells []terminal.Cell, w, y int, state Config) {
	// Draw 3 rows
	// Row 1: TrueColor Reference (Gradient)
	// Row 2: 256-Color Result (Quantized Threshold logic)
	// Row 3: Markers/Text

	barWidth := w - 4
	shieldColor := render.RgbShieldBase
	bgColor := render.RgbBackground // Tokyo Night BG

	// Draw background text first to prove transparency
	bgText := "Shields Up! Shields Up! Shields Up! Shields Up! Shields Up!"
	for i := 0; i < barWidth; i++ {
		char := rune(bgText[i%len(bgText)])
		// Base Layer
		setCell(cells, w, i+2, y, char, render.RGB{80, 80, 100}, bgColor)
		setCell(cells, w, i+2, y+1, char, render.RGB{80, 80, 100}, bgColor)
	}

	for x := 0; x < barWidth; x++ {
		dist := float64(x) / float64(barWidth)

		// --- Row 1: TrueColor Smooth (Center=Transparent -> Edge=Full) ---
		// Imitate cellTrueColor logic: alpha scales with dist
		// falloff := dist * dist // Simple approximation for visualizer (0 at center, 1 at edge)
		tcAlpha := dist * state.Alpha
		tcRGB := applyBlend(state.BlendMode, bgColor, shieldColor, tcAlpha)
		setCell(cells, w, x+2, y, '█', tcRGB, tcRGB)

		// --- Row 2: 256-Color Logic (Threshold) ---
		var final256RGB render.RGB

		if dist < state.RimThreshold {
			// Center: Transparent (keep existing background from text layer above)
			// effectively we do nothing, but for the visualizer we need to calculate
			// what "nothing" looks like (it looks like bgColor)
			final256RGB = bgColor
		} else {
			// Rim: Blend
			blended := applyBlend(state.BlendMode, bgColor, shieldColor, state.Alpha)
			idx256 := terminal.RGBTo256(blended)
			final256RGB = term256ToRGB(idx256)
		}

		// Only draw if not transparent (simulate opacity)
		if dist >= state.RimThreshold {
			setCell(cells, w, x+2, y+1, '█', final256RGB, final256RGB)
		}

		// --- Row 3: Markers ---
		char := ' '
		fg := render.RgbLineNumbers
		if x == 0 {
			char = 'C' // Center
		} else if x == barWidth-1 {
			char = 'E' // Edge
		} else if isApprox(dist, state.RimThreshold, 0.01) {
			char = '^'
			fg = render.RgbCursorError
		}
		setCell(cells, w, x+2, y+2, char, fg, render.RGBBlack)
	}

	printStr(cells, w, barWidth+3, y, "TC", render.RGB{255, 255, 255})
	printStr(cells, w, barWidth+3, y+1, "256", render.RGB{255, 255, 255})
}

func drawPaletteGrid(cells []terminal.Cell, w, startY int, state Config) {
	// Grid:
	// Color Name | Raw | Blended (TC) | Blended (256) + RGB Code

	headers := []string{"Name", "Raw", "Blended (TC)", "Blended (256) -> RGB"}
	colX := []int{2, 15, 25, 45}

	for i, h := range headers {
		printStr(cells, w, colX[i], startY, h, render.RgbLineNumbers)
	}

	y := startY + 2
	for _, nc := range namedColors {
		// 1. Name
		printStr(cells, w, colX[0], y, nc.name, render.RGB{200, 200, 200})

		// 2. Raw Color
		setCell(cells, w, colX[1], y, '█', nc.c, render.RGBBlack)
		setCell(cells, w, colX[1]+1, y, '█', nc.c, render.RGBBlack)
		setCell(cells, w, colX[1]+2, y, '█', nc.c, render.RGBBlack)

		// 3. Blended (TrueColor)
		blendedTC := applyBlend(state.BlendMode, state.BgColor, nc.c, state.Alpha)
		setCell(cells, w, colX[2], y, '█', blendedTC, render.RGBBlack)
		setCell(cells, w, colX[2]+1, y, '█', blendedTC, render.RGBBlack)
		setCell(cells, w, colX[2]+2, y, '█', blendedTC, render.RGBBlack)
		setCell(cells, w, colX[2]+3, y, '█', blendedTC, render.RGBBlack)

		// 4. Blended (256 Simulation)
		idx256 := terminal.RGBTo256(blendedTC)
		quantized := term256ToRGB(idx256)
		setCell(cells, w, colX[3], y, '█', quantized, render.RGBBlack)
		setCell(cells, w, colX[3]+1, y, '█', quantized, render.RGBBlack)
		setCell(cells, w, colX[3]+2, y, '█', quantized, render.RGBBlack)
		setCell(cells, w, colX[3]+3, y, '█', quantized, render.RGBBlack)

		// Show RGB Hex
		hexStr := fmt.Sprintf("#%02X%02X%02X", quantized.R, quantized.G, quantized.B)
		printStr(cells, w, colX[3]+6, y, hexStr, render.RgbLineNumbers)

		y++
	}
}

// Helpers

func applyBlend(mode render.BlendMode, dest, src render.RGB, alpha float64) render.RGB {
	switch mode {
	case render.BlendScreen:
		return render.Screen(dest, src, alpha)
	case render.BlendAdd:
		return render.Add(dest, src, alpha)
	case render.BlendAlpha:
		return render.Blend(dest, src, alpha)
	case render.BlendSoftLight:
		return render.SoftLight(dest, src, alpha)
	case render.BlendOverlay:
		return render.Overlay(dest, src, alpha)
	case render.BlendMax:
		return render.Max(dest, src, alpha)
	default:
		return src
	}
}

func term256ToRGB(index uint8) render.RGB {
	if index < 16 {
		return render.RGB{128, 128, 128} // Approximation
	}
	if index >= 232 {
		g := uint8((int(index)-232)*10 + 8)
		return render.RGB{g, g, g}
	}
	i := int(index) - 16
	r := (i / 36) * 51
	g := ((i / 6) % 6) * 51
	b := (i % 6) * 51
	return render.RGB{uint8(r), uint8(g), uint8(b)}
}

func clearCells(cells []terminal.Cell, bg render.RGB) {
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Bg: bg, Fg: bg}
	}
}

func setCell(cells []terminal.Cell, w, x, y int, r rune, fg, bg render.RGB) {
	if x < 0 || x >= w || y*w+x >= len(cells) {
		return
	}
	cells[y*w+x] = terminal.Cell{Rune: r, Fg: fg, Bg: bg}
}

func printStr(cells []terminal.Cell, w, x, y int, s string, fg render.RGB) {
	for i, r := range s {
		setCell(cells, w, x+i, y, r, fg, render.RGBBlack)
	}
}

func isApprox(a, b, epsilon float64) bool {
	return (a-b) < epsilon && (b-a) < epsilon
}