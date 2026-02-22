package main

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/terminal"
)

// Frame holds per-cell visual data for one animation frame
// All string fields must be exactly Width bytes per row, Height rows
// Palette index encoding: '0'-'9','a'-'f' → 0-15; ' ' → skip (keep existing)
type Frame struct {
	Art  []string // character grid
	Fg   []string // fg palette index per byte position
	Bg   []string // bg palette index per byte position
	Attr []string // 'B'=bold, 'D'=dim, ' '=none
}

// EnemyTemplate defines a species with per-cell palette-driven visuals
type EnemyTemplate struct {
	Name          string
	Width, Height int
	FgPalette     []terminal.RGB
	BgPalette     []terminal.RGB
	AuraColor     terminal.RGB
	AuraRadius    float64
	AuraPulseFreq float64 // Hz
	TicksPerFrame int     // base ticks per frame change
	Frames        []Frame
}

// Enemy represents a placed instance
type Enemy struct {
	X, Y       int
	Template   *EnemyTemplate
	AnimOffset int
	Phase      float64 // aura pulse phase offset (radians)
}

var startTime = time.Now()

// --- Color helpers ---

func scaleRGB(c terminal.RGB, f float64) terminal.RGB {
	if f <= 0 {
		return terminal.Black
	}
	r := float64(c.R) * f
	g := float64(c.G) * f
	b := float64(c.B) * f
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
	r := int(a.R) + int(b.R)
	g := int(a.G) + int(b.G)
	bl := int(a.B) + int(b.B)
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
		return int(b - 'a' + 10)
	}
	return -1
}

// --- Bestiary ---

var bestiary = []EnemyTemplate{
	// ================================================================
	// INFERNAL — 6x3, 5 frames, fast flicker
	// Fg: 0=LemonYellow 1=FlameOrange 2=BrightRed 3=White 4=Amber 5=DarkCrimson 6=Vermilion
	// Bg: 0=DarkAmber 1=BlackRed 2=Red
	// ================================================================
	{
		Name: "INFERNAL", Width: 6, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.LemonYellow, terminal.FlameOrange, terminal.BrightRed,
			terminal.White, terminal.Amber, terminal.DarkCrimson, terminal.Vermilion,
		},
		BgPalette: []terminal.RGB{
			terminal.DarkAmber, terminal.BlackRed, terminal.Red,
		},
		AuraColor: terminal.FlameOrange, AuraRadius: 3.0, AuraPulseFreq: 1.5,
		TicksPerFrame: 2,
		Frames: []Frame{
			{
				Art:  []string{`,*##*,`, `<[@@]>`, ` /||\ `},
				Fg:   []string{`012210`, `412234`, ` 5555 `},
				Bg:   []string{` 0110 `, ` 1221 `, `      `},
				Attr: []string{` BBBB `, `BBBBBB`, `      `},
			},
			{
				Art:  []string{`'*##*'`, `>[@@]<`, ` \||/ `},
				Fg:   []string{`012210`, `412234`, ` 5555 `},
				Bg:   []string{` 0110 `, ` 1221 `, `      `},
				Attr: []string{` BBBB `, `BBBBBB`, `      `},
			},
			{
				Art:  []string{`~*##*~`, `<{@@}>`, ` |/\| `},
				Fg:   []string{`112211`, `412234`, ` 5555 `},
				Bg:   []string{`001100`, ` 1221 `, `      `},
				Attr: []string{`BBBBBB`, `BBBBBB`, `      `},
			},
			{
				Art:  []string{`;*##*;`, `>{@@}<`, ` /||\ `},
				Fg:   []string{`612216`, `412234`, ` 5555 `},
				Bg:   []string{` 0110 `, ` 1221 `, `      `},
				Attr: []string{` BBBB `, `BBBBBB`, `      `},
			},
			{
				Art:  []string{`.*##*.`, `<[@@]>`, ` \  / `},
				Fg:   []string{`112211`, `412234`, ` 5  5 `},
				Bg:   []string{`001100`, ` 1221 `, `  11  `},
				Attr: []string{`BBBBBB`, `BBBBBB`, `      `},
			},
		},
	},

	// ================================================================
	// WRAITH — 5x3, 4 frames, slow phase-shift
	// Fg: 0=PaleLavender 1=ElectricViolet 2=DarkViolet 3=SoftLavender 4=DeepPurple
	// Bg: 0=Obsidian 1=DeepPurple
	// ================================================================
	{
		Name: "WRAITH", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.PaleLavender, terminal.ElectricViolet, terminal.DarkViolet,
			terminal.SoftLavender, terminal.DeepPurple,
		},
		BgPalette: []terminal.RGB{
			terminal.Obsidian, terminal.DeepPurple,
		},
		AuraColor: terminal.DeepPurple, AuraRadius: 2.5, AuraPulseFreq: 0.6,
		TicksPerFrame: 3,
		Frames: []Frame{
			{
				Art:  []string{` .@. `, `(   )`, ` |~| `},
				Fg:   []string{` 010 `, `2   2`, ` 414 `},
				Bg:   []string{` 010 `, `00000`, `     `},
				Attr: []string{` DBD `, `D   D`, ` DBD `},
			},
			{
				Art:  []string{` :@: `, `{   }`, ` }~{ `},
				Fg:   []string{` 310 `, `2   2`, ` 212 `},
				Bg:   []string{` 010 `, `00100`, `     `},
				Attr: []string{` DBD `, `D   D`, ` DBD `},
			},
			{
				Art:  []string{` '@' `, `[   ]`, ` |~| `},
				Fg:   []string{` 310 `, `2   2`, ` 414 `},
				Bg:   []string{` 110 `, `00000`, `     `},
				Attr: []string{`DBBBD`, `D   D`, ` DBD `},
			},
			{
				Art:  []string{` ;@; `, `<   >`, ` {~} `},
				Fg:   []string{` 010 `, `2   2`, ` 212 `},
				Bg:   []string{` 010 `, `01110`, `     `},
				Attr: []string{` DBD `, `D   D`, ` DBD `},
			},
		},
	},

	// ================================================================
	// ARCANE EYE — 5x3, 5 frames, deliberate blink cycle
	// Fg: 0=CeruleanBlue 1=SteelBlue 2=White 3=BrightCyan 4=LightSkyBlue
	//     5=CobaltBlue 6=DodgerBlue 7=SkyTeal
	// Bg: 0=DeepNavy 1=CobaltBlue 2=DodgerBlue
	// ================================================================
	{
		Name: "ARCANE EYE", Width: 5, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.CeruleanBlue, terminal.SteelBlue, terminal.White,
			terminal.BrightCyan, terminal.LightSkyBlue, terminal.CobaltBlue,
			terminal.DodgerBlue, terminal.SkyTeal,
		},
		BgPalette: []terminal.RGB{
			terminal.DeepNavy, terminal.CobaltBlue, terminal.DodgerBlue,
		},
		AuraColor: terminal.CobaltBlue, AuraRadius: 2.5, AuraPulseFreq: 0.8,
		TicksPerFrame: 4,
		Frames: []Frame{
			// Wide open
			{
				Art:  []string{`/---\`, `|(O)|`, `\---/`},
				Fg:   []string{`01110`, `53325`, `01110`},
				Bg:   []string{`00000`, `01210`, `00000`},
				Attr: []string{` BBB `, `BBBBB`, ` BBB `},
			},
			// Open
			{
				Art:  []string{`/---\`, `|(o)|`, `\---/`},
				Fg:   []string{`01110`, `53425`, `01110`},
				Bg:   []string{`00000`, `01110`, `00000`},
				Attr: []string{` BBB `, `BBBBB`, ` BBB `},
			},
			// Narrow slit
			{
				Art:  []string{`/---\`, `|(=)|`, `\---/`},
				Fg:   []string{`01110`, `53725`, `01110`},
				Bg:   []string{`00000`, `01110`, `00000`},
				Attr: []string{` BBB `, `BB BB`, ` BBB `},
			},
			// Open, glow surge
			{
				Art:  []string{`/~~~\`, `|(O)|`, `\~~~/`},
				Fg:   []string{`06660`, `53325`, `06660`},
				Bg:   []string{`00000`, `01210`, `00000`},
				Attr: []string{`BBBBB`, `BBBBB`, `BBBBB`},
			},
			// Blink shut
			{
				Art:  []string{`/---\`, `|===|`, `\---/`},
				Fg:   []string{`01110`, `51115`, `01110`},
				Bg:   []string{`00000`, `00000`, `00000`},
				Attr: []string{` BBB `, `     `, ` BBB `},
			},
		},
	},

	// ================================================================
	// VENOM QUEEN — 6x3, 4 frames, toxic drip
	// Fg: 0=NeonGreen 1=YellowGreen 2=BrightGreen 3=White 4=DarkGreen 5=Lime
	// Bg: 0=BlackGreen 1=DarkGreen
	// ================================================================
	{
		Name: "VENOM QUEEN", Width: 6, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.NeonGreen, terminal.YellowGreen, terminal.BrightGreen,
			terminal.White, terminal.DarkGreen, terminal.Lime,
		},
		BgPalette: []terminal.RGB{
			terminal.BlackGreen, terminal.DarkGreen,
		},
		AuraColor: terminal.NeonGreen, AuraRadius: 2.0, AuraPulseFreq: 1.0,
		TicksPerFrame: 3,
		Frames: []Frame{
			{
				Art:  []string{`/~oo~\`, `{+##+}`, ` \,,/ `},
				Fg:   []string{`002200`, `121121`, ` 0550 `},
				Bg:   []string{` 0110 `, `001100`, `      `},
				Attr: []string{`DBBBD `, `BBBBBB`, ` DDDD `},
			},
			{
				Art:  []string{`\~oo~/`, `<+##+>`, `  ,,  `},
				Fg:   []string{`002200`, `121121`, `  55  `},
				Bg:   []string{` 0110 `, `001100`, `  00  `},
				Attr: []string{`DBBBD `, `BBBBBB`, `  DD  `},
			},
			{
				Art:  []string{`|~oo~|`, `{+##+}`, ` |,,| `},
				Fg:   []string{`102201`, `121121`, ` 0550 `},
				Bg:   []string{`00110 `, `001100`, `      `},
				Attr: []string{` BBBB `, `BBBBBB`, ` DDDD `},
			},
			{
				Art:  []string{`/~oo~\`, `>+##+<`, ` \,,/ `},
				Fg:   []string{`002200`, `521125`, ` 0550 `},
				Bg:   []string{` 0110 `, `101101`, `      `},
				Attr: []string{`DBBBD `, `BBBBBB`, ` DDDD `},
			},
		},
	},

	// ================================================================
	// IRON REAVER — 6x3, 4 frames, mechanical chop
	// Fg: 0=Silver 1=CoolSilver 2=IronGray 3=BrightRed 4=White 5=DarkGray
	// Bg: 0=DarkSlate 1=BlackRed 2=Gunmetal
	// ================================================================
	{
		Name: "IRON REAVER", Width: 6, Height: 3,
		FgPalette: []terminal.RGB{
			terminal.Silver, terminal.CoolSilver, terminal.IronGray,
			terminal.BrightRed, terminal.White, terminal.DarkGray,
		},
		BgPalette: []terminal.RGB{
			terminal.DarkSlate, terminal.BlackRed, terminal.Gunmetal,
		},
		AuraColor: terminal.IronGray, AuraRadius: 1.5, AuraPulseFreq: 2.0,
		TicksPerFrame: 2,
		Frames: []Frame{
			// Blades open
			{
				Art:  []string{`\-@@-/`, `|[##]|`, `/-  -\`},
				Fg:   []string{`023320`, `213312`, `02  20`},
				Bg:   []string{`201102`, `001100`, `20  02`},
				Attr: []string{`BBBBBB`, `BBBBBB`, `BB  BB`},
			},
			// Blades swap
			{
				Art:  []string{`/-@@-\`, `|{##}|`, `\-  -/`},
				Fg:   []string{`023320`, `213312`, `02  20`},
				Bg:   []string{`201102`, `001100`, `20  02`},
				Attr: []string{`BBBBBB`, `BBBBBB`, `BB  BB`},
			},
			// Blades straight, core pulse
			{
				Art:  []string{`|-@@-|`, `|[##]|`, `|-  -|`},
				Fg:   []string{`223322`, `243342`, `22  22`},
				Bg:   []string{`001100`, `011110`, `00  00`},
				Attr: []string{`BBBBBB`, `BBBBBB`, `BB  BB`},
			},
			// Strike pose
			{
				Art:  []string{`\=@@=\`, `>[##]<`, `/=  =/`},
				Fg:   []string{`043340`, `413314`, `04  40`},
				Bg:   []string{`201102`, `101101`, `20  02`},
				Attr: []string{`BBBBBB`, `BBBBBB`, `BB  BB`},
			},
		},
	},
}

var enemies []Enemy

func main() {
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
	spacing := 12
	// Vertical: entity(3) + gap(1) + label(1) + row gap(2) = 7
	rowHeight := 7

	startX, startY := 4, 4
	currX, currY := startX, startY

	for i := range bestiary {
		t := &bestiary[i]

		// Wrap row
		if currX+t.Width+int(t.AuraRadius)+2 > w {
			currX = startX
			currY += rowHeight
		}

		// Stop if off screen
		if currY+t.Height+2 > h {
			break
		}

		enemies = append(enemies, Enemy{
			X:          currX,
			Y:          currY,
			Template:   t,
			AnimOffset: i * 3,
			Phase:      float64(i) * 1.3,
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

	// Pass 1: aura glow (bg only, beneath sprites)
	for i := range enemies {
		renderAura(cells, w, h, &enemies[i], now)
	}

	// Pass 2: sprite art (fg, bg, char, attr)
	for i := range enemies {
		renderSprite(cells, w, h, &enemies[i], tick)
	}

	// Pass 3: labels beneath each entity
	for i := range enemies {
		e := &enemies[i]
		labelY := e.Y + e.Template.Height + 1
		labelX := e.X + (e.Template.Width-len(e.Template.Name))/2
		if len(e.Template.Name) > e.Template.Width {
			labelX = e.X - (len(e.Template.Name)-e.Template.Width)/2
		}
		drawText(cells, w, h, labelX, labelY, e.Template.Name, e.Template.FgPalette[0], terminal.AttrDim)
	}

	// Title
	title := " TOWER DEFENSE BESTIARY "
	titleX := (w - len(title)) / 2
	drawText(cells, w, h, max(0, titleX), 1, title, terminal.White, terminal.AttrBold)

	// Subtitle
	sub := "Per-cell palette | Radial aura | Multi-frame animation"
	subX := (w - len(sub)) / 2
	drawText(cells, w, h, max(0, subX), 2, sub, terminal.DimGray, terminal.AttrNone)

	// Footer
	footer := " ESC / Q to quit "
	footX := (w - len(footer)) / 2
	drawText(cells, w, h, max(0, footX), h-1, footer, terminal.SlateGray, terminal.AttrDim)

	term.Flush(cells, w, h)
}

// renderAura paints radial elliptical glow around entity bounds
func renderAura(cells []terminal.Cell, w, h int, e *Enemy, now time.Time) {
	t := e.Template
	if t.AuraRadius <= 0 {
		return
	}

	elapsed := now.Sub(startTime).Seconds()

	// Pulse: oscillate intensity
	pulse := 0.55 + 0.45*math.Sin(elapsed*t.AuraPulseFreq*2*math.Pi+e.Phase)

	// Breathing offset: subtle center sway
	breathX := math.Sin(elapsed*t.AuraPulseFreq*math.Pi+e.Phase) * 0.3
	breathY := math.Cos(elapsed*t.AuraPulseFreq*0.7*math.Pi+e.Phase) * 0.15

	cx := float64(e.X) + float64(t.Width)/2.0 + breathX
	cy := float64(e.Y) + float64(t.Height)/2.0 + breathY

	// Elliptical radii: wider X, compressed Y for char aspect ratio
	rx := float64(t.Width)/2.0 + t.AuraRadius
	ry := float64(t.Height)/2.0 + t.AuraRadius*0.55

	invRxSq := 1.0 / (rx * rx)
	invRySq := 1.0 / (ry * ry)

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

			// Smooth falloff: cubic for soft edges
			dist := math.Sqrt(distSq)
			falloff := 1.0 - dist
			alpha := falloff * falloff * falloff * pulse * 0.65

			if alpha < 0.01 {
				continue
			}

			idx := sy*w + sx
			cells[idx].Bg = addRGB(cells[idx].Bg, scaleRGB(t.AuraColor, alpha))
		}
	}
}

// renderSprite paints the current animation frame with per-cell palette lookup
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
			ch := rune(line[x])

			// Bg: always check, even for space chars (allows invisible-fg + visible-bg)
			if y < len(frame.Bg) && x < len(frame.Bg[y]) {
				pi := paletteIdx(frame.Bg[y][x])
				if pi >= 0 && pi < len(t.BgPalette) {
					cells[idx].Bg = t.BgPalette[pi]
				}
			}

			if ch == ' ' {
				continue
			}

			cells[idx].Rune = ch

			// Fg
			if y < len(frame.Fg) && x < len(frame.Fg[y]) {
				pi := paletteIdx(frame.Fg[y][x])
				if pi >= 0 && pi < len(t.FgPalette) {
					cells[idx].Fg = t.FgPalette[pi]
				}
			}

			// Attr
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

func drawText(cells []terminal.Cell, w, h, x, y int, text string, fg terminal.RGB, attr terminal.Attr) {
	if y < 0 || y >= h {
		return
	}
	for i, r := range text {
		sx := x + i
		if sx >= 0 && sx < w {
			cells[y*w+sx] = terminal.Cell{
				Rune:  r,
				Fg:    fg,
				Bg:    terminal.Black,
				Attrs: attr,
			}
		}
	}
}
