package main

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Frame holds per-cell visual data for one animation frame
// Palette index encoding: '0'-'9','a'-'f' → 0-15; ' ' → skip
type Frame struct {
	Art  []string
	Fg   []string
	Bg   []string
	Attr []string
}

type borderCell struct{ x, y int }

type EnemyTemplate struct {
	Name          string
	Width, Height int
	FgPalette     []terminal.RGB
	BgPalette     []terminal.RGB

	// Radial aura
	AuraColor      terminal.RGB
	AuraRadius     float64
	AuraPulseFreq  float64 // Hz
	AuraRotSpeed   float64 // Hz, 0 = static omnidirectional
	AuraFocusWidth float64 // 0.1 = tight beam, 1.0 = gentle spread

	// Programmatic border rotation
	BorderRotSpeed  float64 // Hz, 0 = off
	BorderHighlight terminal.RGB
	BorderWidth     int // highlight width in perimeter cells

	TicksPerFrame int
	Frames        []Frame

	// Computed at init
	borderPerim []borderCell
}

type Enemy struct {
	X, Y       int
	Template   *EnemyTemplate
	AnimOffset int
	Phase      float64
}

var startTime = time.Now()

// --- Color helpers ---

func scaleRGB(c terminal.RGB, f float64) terminal.RGB {
	if f <= 0 {
		return terminal.Black
	}
	r, g, b := float64(c.R)*f, float64(c.G)*f, float64(c.B)*f
	if r > 255 {
		r = 255
	}
	if g > 255 {
		g = 255
	}
	if b > 255 {
		b = 255
	}
	return terminal.RGB{R: uint8(r), G: uint8(g), B: uint8(b)}
}

func addRGB(a, b terminal.RGB) terminal.RGB {
	r, g, bl := int(a.R)+int(b.R), int(a.G)+int(b.G), int(a.B)+int(b.B)
	if r > 255 {
		r = 255
	}
	if g > 255 {
		g = 255
	}
	if bl > 255 {
		bl = 255
	}
	return terminal.RGB{R: uint8(r), G: uint8(g), B: uint8(bl)}
}

func paletteIdx(b byte) int {
	if b >= '0' && b <= '9' {
		return int(b - '0')
	}
	if b >= 'a' && b <= 'f' {
		return int(b-'a') + 10
	}
	return -1
}

func computePerimeter(w, h int) []borderCell {
	cells := make([]borderCell, 0, 2*w+2*(h-2))
	for x := 0; x < w; x++ {
		cells = append(cells, borderCell{x, 0})
	}
	for y := 1; y < h-1; y++ {
		cells = append(cells, borderCell{w - 1, y})
	}
	for x := w - 1; x >= 0; x-- {
		cells = append(cells, borderCell{x, h - 1})
	}
	for y := h - 2; y >= 1; y-- {
		cells = append(cells, borderCell{0, y})
	}
	return cells
}

// --- Bestiary ---

var bestiary = []EnemyTemplate{

	// ================================================================
	// VOID EYE — 5x3, slow contemplative blink
	// Deep ocean abyss. Slow rotating aura suggests submerged current
	// Edge: [---] solid opaque box
	// Fg: 0=DimGray 1=SteelBlue 2=White 3=CeruleanBlue 4=NavyBlue
	//     5=LightSkyBlue 6=CobaltBlue 7=DodgerBlue
	// Bg: 0=DeepNavy 1=Gunmetal 2=CobaltBlue
	// ================================================================
	{
		Name: "VOID EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.DimGray, terminal.SteelBlue, terminal.White,
			terminal.CeruleanBlue, terminal.NavyBlue, terminal.LightSkyBlue,
			terminal.CobaltBlue, terminal.DodgerBlue,
		},
		BgPalette: []terminal.RGB{
			terminal.DeepNavy, terminal.Gunmetal, terminal.CobaltBlue,
		},
		AuraColor: terminal.CobaltBlue, AuraRadius: 2.5, AuraPulseFreq: 0.5,
		AuraRotSpeed: 0.15, AuraFocusWidth: 0.7,
		TicksPerFrame: 4,
		Frames: []Frame{
			{ // Wide open — bright pupil, deep iris bg
				Art:  []string{"[---]", "|(O)|", "[---]"},
				Fg:   []string{"01110", "43234", "01110"},
				Bg:   []string{"00000", "01210", "00000"},
				Attr: []string{" BBB ", " BBB ", " BBB "},
			},
			{ // Open — dimmer pupil
				Art:  []string{"[---]", "|(o)|", "[---]"},
				Fg:   []string{"01110", "43534", "01110"},
				Bg:   []string{"00000", "01110", "00000"},
				Attr: []string{" BBB ", " BBB ", " BBB "},
			},
			{ // Narrow slit
				Art:  []string{"[===]", "|(=)|", "[===]"},
				Fg:   []string{"06660", "43634", "06660"},
				Bg:   []string{"00000", "01110", "00000"},
				Attr: []string{" BBB ", "  B  ", " BBB "},
			},
			{ // Glow surge — border activates
				Art:  []string{"[~~~]", "|(O)|", "[~~~]"},
				Fg:   []string{"07770", "43234", "07770"},
				Bg:   []string{"00100", "01210", "00100"},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{ // Shut
				Art:  []string{"[---]", "|===|", "[---]"},
				Fg:   []string{"01110", "46664", "01110"},
				Bg:   []string{"00000", "00000", "00000"},
				Attr: []string{" BBB ", "     ", " BBB "},
			},
		},
	},

	// ================================================================
	// FLAME EYE — 5x3, aggressive flicker
	// Edge: # corners, thick hot border
	// Fg: 0=LemonYellow 1=FlameOrange 2=White 3=BrightRed 4=Amber
	//     5=DarkCrimson 6=Vermilion 7=WarmOrange
	// Bg: 0=BlackRed 1=DarkAmber 2=Red
	// ================================================================
	{
		Name: "FLAME EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.LemonYellow, terminal.FlameOrange, terminal.White,
			terminal.BrightRed, terminal.Amber, terminal.DarkCrimson,
			terminal.Vermilion, terminal.WarmOrange,
		},
		BgPalette: []terminal.RGB{
			terminal.BlackRed, terminal.DarkAmber, terminal.Red,
		},
		AuraColor: terminal.FlameOrange, AuraRadius: 2.5, AuraPulseFreq: 2.0,
		TicksPerFrame: 2,
		Frames: []Frame{
			{ // Base
				Art:  []string{"#---#", "|<@>|", "#---#"},
				Fg:   []string{"51115", "54245", "51115"},
				Bg:   []string{"00000", "01210", "00000"},
				Attr: []string{"B   B", " BBB ", "B   B"},
			},
			{ // Flare — star pupil, border hash pattern
				Art:  []string{"#-#-#", "|{*}|", "#-#-#"},
				Fg:   []string{"51615", "57275", "51615"},
				Bg:   []string{"01010", "01210", "01010"},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
			{ // Dim — small pupil
				Art:  []string{"#---#", "|<o>|", "#---#"},
				Fg:   []string{"51115", "54745", "51115"},
				Bg:   []string{"00000", "01110", "00000"},
				Attr: []string{"B   B", " BBB ", "B   B"},
			},
			{ // Bright — full glow
				Art:  []string{"#===#", "|<O>|", "#===#"},
				Fg:   []string{"50005", "54245", "50005"},
				Bg:   []string{"01110", "01210", "01110"},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// ================================================================
	// FROST EYE — 5x3, crystalline pulse
	// Edge: * decorative corners, icy
	// Fg: 0=BrightCyan 1=White 2=LightSkyBlue 3=CeruleanBlue
	//     4=SteelBlue 5=CoolSilver 6=AliceBlue 7=PaleCyan
	// Bg: 0=DeepNavy 1=CobaltBlue 2=SteelBlue
	// ================================================================
	{
		Name: "FROST EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.BrightCyan, terminal.White, terminal.LightSkyBlue,
			terminal.CeruleanBlue, terminal.SteelBlue, terminal.CoolSilver,
			terminal.AliceBlue, terminal.PaleCyan,
		},
		BgPalette: []terminal.RGB{
			terminal.DeepNavy, terminal.CobaltBlue, terminal.SteelBlue,
		},
		AuraColor: terminal.BrightCyan, AuraRadius: 2.5, AuraPulseFreq: 0.4,
		AuraRotSpeed: 0.2, AuraFocusWidth: 0.4,
		TicksPerFrame: 4,
		Frames: []Frame{
			{ // Open — diamond pupil
				Art:  []string{"*---*", "|<O>|", "*---*"},
				Fg:   []string{"43334", "30103", "43334"},
				Bg:   []string{"00000", "01210", "00000"},
				Attr: []string{"B   B", " BBB ", "B   B"},
			},
			{ // Crystal shift — border sparkle
				Art:  []string{"*-+-*", "|(O)|", "*-+-*"},
				Fg:   []string{"43134", "30103", "43134"},
				Bg:   []string{"00100", "01210", "00100"},
				Attr: []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{ // Narrow — slit
				Art:  []string{"*---*", "|{=}|", "*---*"},
				Fg:   []string{"43334", "30534", "43334"},
				Bg:   []string{"00000", "01110", "00000"},
				Attr: []string{"B   B", "  B  ", "B   B"},
			},
			{ // Surge — full ice bloom
				Art:  []string{"*~+~*", "|(O)|", "*~+~*"},
				Fg:   []string{"40104", "30103", "40104"},
				Bg:   []string{"01210", "01210", "01210"},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// ================================================================
	// STORM EYE — 6x3, electric, programmatic rotating border
	// Edge: + corners with rotating bg highlight
	// Fg: 0=BrightCyan 1=CeruleanBlue 2=White 3=LemonYellow
	//     4=SteelBlue 5=DodgerBlue 6=SkyTeal 7=LightSkyBlue
	// Bg: 0=DeepNavy 1=CobaltBlue
	// ================================================================
	{
		Name: "STORM EYE", Width: 6, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.BrightCyan, terminal.CeruleanBlue, terminal.White,
			terminal.LemonYellow, terminal.SteelBlue, terminal.DodgerBlue,
			terminal.SkyTeal, terminal.LightSkyBlue,
		},
		BgPalette: []terminal.RGB{
			terminal.DeepNavy, terminal.CobaltBlue,
		},
		AuraColor: terminal.BrightCyan, AuraRadius: 3.0, AuraPulseFreq: 1.2,
		AuraRotSpeed: 0.8, AuraFocusWidth: 0.5,
		BorderRotSpeed: 1.0, BorderHighlight: terminal.BrightCyan, BorderWidth: 3,
		TicksPerFrame: 4,
		Frames: []Frame{
			{ // Wide open
				Art:  []string{"+~~~~+", "|(OO)|", "+~~~~+"},
				Fg:   []string{"400004", "412214", "400004"},
				Bg:   []string{"000000", "011110", "000000"},
				Attr: []string{"BBBBBB", " BBBB ", "BBBBBB"},
			},
			{ // Narrow
				Art:  []string{"+~~~~+", "|(==)|", "+~~~~+"},
				Fg:   []string{"400004", "416614", "400004"},
				Bg:   []string{"000000", "011110", "000000"},
				Attr: []string{"BBBBBB", " B  B ", "BBBBBB"},
			},
			{ // Surge — interior flash
				Art:  []string{"+~~~~+", "|{OO}|", "+~~~~+"},
				Fg:   []string{"430034", "432234", "430034"},
				Bg:   []string{"001100", "011110", "001100"},
				Attr: []string{"BBBBBB", "BBBBBB", "BBBBBB"},
			},
		},
	},

	// ================================================================
	// BLOOD EYE — 5x3, veined, pulsing
	// Edge: > < pointed sides
	// Fg: 0=DarkCrimson 1=BrightRed 2=White 3=Vermilion
	//     4=Coral 5=Red 6=Salmon 7=LightCoral
	// Bg: 0=BlackRed 1=DarkCrimson 2=Red
	// ================================================================
	{
		Name: "BLOOD EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.DarkCrimson, terminal.BrightRed, terminal.White,
			terminal.Vermilion, terminal.Coral, terminal.Red,
			terminal.Salmon, terminal.LightCoral,
		},
		BgPalette: []terminal.RGB{
			terminal.BlackRed, terminal.DarkCrimson, terminal.Red,
		},
		AuraColor: terminal.DarkCrimson, AuraRadius: 2.0, AuraPulseFreq: 1.2,
		TicksPerFrame: 3,
		Frames: []Frame{
			{ // Base — steady gaze
				Art:  []string{">---<", "|(X)|", ">---<"},
				Fg:   []string{"31113", "05250", "31113"},
				Bg:   []string{"00000", "01210", "00000"},
				Attr: []string{"B   B", " BBB ", "B   B"},
			},
			{ // Vein pulse — border throbs
				Art:  []string{">===<", "|(X)|", ">===<"},
				Fg:   []string{"35553", "05250", "35553"},
				Bg:   []string{"01110", "01210", "01110"},
				Attr: []string{"BBBBB", " BBB ", "BBBBB"},
			},
			{ // Slit — angry narrow
				Art:  []string{">---<", "|-X-|", ">---<"},
				Fg:   []string{"31113", "05250", "31113"},
				Bg:   []string{"00000", "01110", "00000"},
				Attr: []string{"B   B", "  B  ", "B   B"},
			},
			{ // Full dilate — threat display
				Art:  []string{">-#-<", "|(O)|", ">-#-<"},
				Fg:   []string{"31513", "04240", "31513"},
				Bg:   []string{"00100", "01210", "00100"},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},

	// ================================================================
	// GOLDEN EYE — 6x3, warm amber, programmatic rotating border
	// Edge: | sides, = top/bottom. Slow warm rotation
	// Fg: 0=Gold 1=Amber 2=White 3=LemonYellow 4=DarkGold
	//     5=PaleGold 6=Buttercream 7=WarmOrange
	// Bg: 0=DarkAmber 1=Amber 2=Gold
	// ================================================================
	{
		Name: "GOLDEN EYE", Width: 6, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.Gold, terminal.Amber, terminal.White,
			terminal.LemonYellow, terminal.DarkGold, terminal.PaleGold,
			terminal.Buttercream, terminal.WarmOrange,
		},
		BgPalette: []terminal.RGB{
			terminal.DarkAmber, terminal.Amber, terminal.Gold,
		},
		AuraColor: terminal.Amber, AuraRadius: 2.5, AuraPulseFreq: 0.6,
		AuraRotSpeed: -0.3, AuraFocusWidth: 0.6,
		BorderRotSpeed: 0.4, BorderHighlight: terminal.Gold, BorderWidth: 3,
		TicksPerFrame: 5,
		Frames: []Frame{
			{ // Regal open
				Art:  []string{"|====|", "|(OO)|", "|====|"},
				Fg:   []string{"400004", "412214", "400004"},
				Bg:   []string{"000000", "011110", "000000"},
				Attr: []string{"BBBBBB", " BBBB ", "BBBBBB"},
			},
			{ // Shimmer — center hash
				Art:  []string{"|=##=|", "|{OO}|", "|=##=|"},
				Fg:   []string{"403304", "712217", "403304"},
				Bg:   []string{"001100", "011110", "001100"},
				Attr: []string{"BBBBBB", "BBBBBB", "BBBBBB"},
			},
			{ // Narrow
				Art:  []string{"|====|", "|(==)|", "|====|"},
				Fg:   []string{"400004", "415514", "400004"},
				Bg:   []string{"000000", "011110", "000000"},
				Attr: []string{"BBBBBB", " B  B ", "BBBBBB"},
			},
			{ // Crown — full glow
				Art:  []string{"|~##~|", "|(OO)|", "|~##~|"},
				Fg:   []string{"433334", "412214", "433334"},
				Bg:   []string{"012210", "012210", "012210"},
				Attr: []string{"BBBBBB", "BBBBBB", "BBBBBB"},
			},
		},
	},

	// ================================================================
	// ABYSS EYE — 5x3, dimensional rift, transparent corners
	// Edge: . ' corners (NO bg), - | sides (with bg)
	// Corner cells show aura through, creating bleed effect
	// Fg: 0=PaleLavender 1=ElectricViolet 2=White 3=SoftLavender
	//     4=DarkViolet 5=MutedPurple 6=DeepPurple 7=Orchid
	// Bg: 0=Obsidian 1=DeepPurple
	// ================================================================
	{
		Name: "ABYSS EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.PaleLavender, terminal.ElectricViolet, terminal.White,
			terminal.SoftLavender, terminal.DarkViolet, terminal.MutedPurple,
			terminal.DeepPurple, terminal.Orchid,
		},
		BgPalette: []terminal.RGB{
			terminal.Obsidian, terminal.DeepPurple,
		},
		AuraColor: terminal.DeepPurple, AuraRadius: 3.0, AuraPulseFreq: 0.5,
		AuraRotSpeed: 0.25, AuraFocusWidth: 0.3,
		TicksPerFrame: 4,
		Frames: []Frame{
			{ // Open — rift visible
				Art:  []string{".---.", "|(O)|", "'---'"},
				Fg:   []string{"64446", "41214", "64446"},
				Bg:   []string{" 000 ", "01110", " 000 "},
				Attr: []string{" BBB ", " BBB ", " BBB "},
			},
			{ // Shift — bracket iris
				Art:  []string{".---.", "|{O}|", "'---'"},
				Fg:   []string{"64446", "47274", "64446"},
				Bg:   []string{" 000 ", "01110", " 000 "},
				Attr: []string{" BBB ", " BBB ", " BBB "},
			},
			{ // Phase — border flickers dim
				Art:  []string{".~~~.", "|[O]|", "'~~~'"},
				Fg:   []string{"65556", "41214", "65556"},
				Bg:   []string{" 111 ", "01110", " 111 "},
				Attr: []string{"DBBBD", " BBB ", "DBBBD"},
			},
			{ // Rift surge — core flares
				Art:  []string{".~~~.", "|(O)|", "'~~~'"},
				Fg:   []string{"61116", "41214", "61116"},
				Bg:   []string{" 111 ", "01110", " 111 "},
				Attr: []string{"BBBBB", "BBBBB", "BBBBB"},
			},
		},
	},
}

var enemies []Enemy

func initBestiary() {
	for i := range bestiary {
		t := &bestiary[i]
		if t.BorderRotSpeed != 0 {
			t.borderPerim = computePerimeter(t.Width, t.Height)
		}
		if t.BorderWidth == 0 {
			t.BorderWidth = 2
		}
	}
}

func main() {
	initBestiary()

	term := terminal.New()
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()

	w, h := term.Size()
	layoutEnemies(w, h)

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			term.PostEvent(terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone})
		}
	}()

	tickCount := 0
	renderFrame(term, tickCount)

	for {
		ev := term.PollEvent()
		switch ev.Type {
		case terminal.EventClosed, terminal.EventError:
			return
		case terminal.EventKey:
			if ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyCtrlC || ev.Rune == 'q' || ev.Rune == 'Q' {
				return
			}
			if ev.Key == terminal.KeyNone {
				tickCount++
				renderFrame(term, tickCount)
			}
		case terminal.EventResize:
			layoutEnemies(ev.Width, ev.Height)
			renderFrame(term, tickCount)
		}
	}
}

func layoutEnemies(w, h int) {
	enemies = nil
	spacing := 14
	rowHeight := 7 // entity(3) + gap(1) + label(1) + row_gap(2)
	startX, startY := 4, 4
	currX, currY := startX, startY

	for i := range bestiary {
		t := &bestiary[i]
		needed := t.Width + int(t.AuraRadius) + 2
		if t.BorderRotSpeed != 0 {
			needed += 2
		}

		if currX+needed > w {
			currX = startX
			currY += rowHeight
		}
		if currY+t.Height+2 > h {
			break
		}

		enemies = append(enemies, Enemy{
			X:          currX,
			Y:          currY,
			Template:   t,
			AnimOffset: i * 3,
			Phase:      float64(i) * 1.1,
		})
		currX += t.Width + spacing
	}
}

func renderFrame(term terminal.Terminal, tick int) {
	w, h := term.Size()
	if w <= 0 || h <= 0 {
		return
	}

	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Bg: terminal.Black}
	}

	now := time.Now()

	// Pass 1: directional aura glow
	for i := range enemies {
		renderAura(cells, w, h, &enemies[i], now)
	}

	// Pass 2: sprite frame (fg, bg, char, attr)
	for i := range enemies {
		renderSprite(cells, w, h, &enemies[i], tick)
	}

	// Pass 3: programmatic border rotation (additive, on top of sprite bg)
	for i := range enemies {
		renderBorderHighlight(cells, w, h, &enemies[i], now)
	}

	// Pass 4: labels
	for i := range enemies {
		e := &enemies[i]
		labelY := e.Y + e.Template.Height + 1
		labelX := e.X + (e.Template.Width-len(e.Template.Name))/2
		drawText(cells, w, h, labelX, labelY, e.Template.Name, e.Template.FgPalette[0], terminal.AttrDim)
	}

	title := " TOWER DEFENSE — ARCANE SENTINELS "
	drawText(cells, w, h, max(0, (w-len(title))/2), 1, title, terminal.White, terminal.AttrBold)

	sub := "Per-cell palette | Directional aura | Rotating borders"
	drawText(cells, w, h, max(0, (w-len(sub))/2), 2, sub, terminal.DimGray, terminal.AttrNone)

	footer := " ESC / Q to quit "
	drawText(cells, w, h, max(0, (w-len(footer))/2), h-1, footer, terminal.SlateGray, terminal.AttrDim)

	term.Flush(cells, w, h)
}

// renderAura paints elliptical glow with optional rotating directional modulation
func renderAura(cells []terminal.Cell, w, h int, e *Enemy, now time.Time) {
	t := e.Template
	if t.AuraRadius <= 0 {
		return
	}

	elapsed := now.Sub(startTime).Seconds()

	// Base pulse
	pulse := 0.55 + 0.45*math.Sin(elapsed*t.AuraPulseFreq*2*math.Pi+e.Phase)

	// Breathing offset
	breathX := math.Sin(elapsed*t.AuraPulseFreq*math.Pi+e.Phase) * 0.3
	breathY := math.Cos(elapsed*t.AuraPulseFreq*0.7*math.Pi+e.Phase) * 0.15

	cx := float64(e.X) + float64(t.Width)/2.0 + breathX
	cy := float64(e.Y) + float64(t.Height)/2.0 + breathY

	rx := float64(t.Width)/2.0 + t.AuraRadius
	ry := float64(t.Height)/2.0 + t.AuraRadius*0.55

	invRxSq := 1.0 / (rx * rx)
	invRySq := 1.0 / (ry * ry)

	hasRot := t.AuraRotSpeed != 0
	var rotAngle float64
	if hasRot {
		rotAngle = elapsed*t.AuraRotSpeed*2*math.Pi + e.Phase
	}

	startX := max(0, int(cx-rx)-1)
	endX := min(w-1, int(cx+rx)+1)
	startY := max(0, int(cy-ry)-1)
	endY := min(h-1, int(cy+ry)+1)

	for sy := startY; sy <= endY; sy++ {
		for sx := startX; sx <= endX; sx++ {
			dx := float64(sx) - cx
			dy := float64(sy) - cy
			distSq := dx*dx*invRxSq + dy*dy*invRySq
			if distSq > 1.0 {
				continue
			}

			dist := math.Sqrt(distSq)
			falloff := 1.0 - dist
			alpha := falloff * falloff * falloff * pulse * 0.65

			// Directional modulation
			if hasRot && alpha > 0.001 {
				// Aspect-corrected angle for elliptical shape
				cellAngle := math.Atan2(dy*(rx/ry), dx)
				angleDiff := cellAngle - rotAngle
				dirFactor := (math.Cos(angleDiff) + 1.0) / 2.0
				if t.AuraFocusWidth > 0 && t.AuraFocusWidth < 1.0 {
					dirFactor = math.Pow(dirFactor, 1.0/t.AuraFocusWidth)
				}
				// Blend: retain base glow, amplify in beam direction
				alpha *= 0.25 + 0.75*dirFactor
			}

			if alpha < 0.01 {
				continue
			}

			idx := sy*w + sx
			cells[idx].Bg = addRGB(cells[idx].Bg, scaleRGB(t.AuraColor, alpha))
		}
	}
}

// renderSprite draws current animation frame with per-cell palette lookup
func renderSprite(cells []terminal.Cell, w, h int, e *Enemy, tick int) {
	t := e.Template
	frameIdx := ((tick + e.AnimOffset) / t.TicksPerFrame) % len(t.Frames)
	frame := &t.Frames[frameIdx]

	for y := 0; y < len(frame.Art) && y < t.Height; y++ {
		line := frame.Art[y]
		for x := 0; x < len(line) && x < t.Width; x++ {
			sx := e.X + x
			sy := e.Y + y
			if sx < 0 || sx >= w || sy < 0 || sy >= h {
				continue
			}

			idx := sy*w + sx

			// Bg — applied even for space chars (allows bg-only cells)
			if y < len(frame.Bg) && x < len(frame.Bg[y]) {
				pi := paletteIdx(frame.Bg[y][x])
				if pi >= 0 && pi < len(t.BgPalette) {
					cells[idx].Bg = t.BgPalette[pi]
				}
			}

			ch := rune(line[x])
			if ch == ' ' {
				continue
			}

			cells[idx].Rune = ch

			if y < len(frame.Fg) && x < len(frame.Fg[y]) {
				pi := paletteIdx(frame.Fg[y][x])
				if pi >= 0 && pi < len(t.FgPalette) {
					cells[idx].Fg = t.FgPalette[pi]
				}
			}

			if y < len(frame.Attr) && x < len(frame.Attr[y]) {
				switch frame.Attr[y][x] {
				case 'B':
					cells[idx].Attrs = terminal.AttrBold
				case 'D':
					cells[idx].Attrs = terminal.AttrDim
				}
			}
		}
	}
}

// renderBorderHighlight overlays rotating highlight on perimeter cells
func renderBorderHighlight(cells []terminal.Cell, w, h int, e *Enemy, now time.Time) {
	t := e.Template
	if t.BorderRotSpeed == 0 || len(t.borderPerim) == 0 {
		return
	}

	elapsed := now.Sub(startTime).Seconds()
	n := float64(len(t.borderPerim))

	// Current position along perimeter (fractional, wrapping)
	pos := elapsed*math.Abs(t.BorderRotSpeed)*n + e.Phase*n/6.28
	pos = pos - math.Floor(pos/n)*n

	bw := float64(t.BorderWidth)

	for i, cell := range t.borderPerim {
		fi := float64(i)

		// Distance to primary highlight (wrapping)
		d := math.Abs(fi - pos)
		if d > n/2 {
			d = n - d
		}

		// Distance to opposing highlight (diametrically opposite)
		oppPos := pos + n/2
		if oppPos >= n {
			oppPos -= n
		}
		dOpp := math.Abs(fi - oppPos)
		if dOpp > n/2 {
			dOpp = n - dOpp
		}

		minDist := math.Min(d, dOpp)
		if minDist >= bw {
			continue
		}

		// Quadratic falloff
		alpha := 1.0 - minDist/bw
		alpha = alpha * alpha * 0.9

		sx := e.X + cell.x
		sy := e.Y + cell.y
		if sx >= 0 && sx < w && sy >= 0 && sy < h {
			idx := sy*w + sx
			cells[idx].Bg = addRGB(cells[idx].Bg, scaleRGB(t.BorderHighlight, alpha))
		}
	}
}

func drawText(cells []terminal.Cell, w, h, x, y int, text string, fg terminal.RGB, attr terminal.Attr) {
	if y < 0 || y >= h {
		return
	}
	for i, r := range text {
		sx := x + i
		if sx >= 0 && sx < w {
			cells[y*w+sx] = terminal.Cell{Rune: r, Fg: fg, Bg: terminal.Black, Attrs: attr}
		}
	}
}