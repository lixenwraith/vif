package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

const aspectRatio = 2.1

// ColorPalette defines gradient stops for ember effect
type ColorPalette struct {
	Name               string
	CoreHot, CoreEmber terminal.RGB
	MidHot, MidEmber   terminal.RGB
	EdgeHot, EdgeEmber terminal.RGB
	RingColor          terminal.RGB
}

var palettes = []ColorPalette{
	{
		Name:      "Solar",
		CoreHot:   terminal.RGB{R: 255, G: 255, B: 250},
		CoreEmber: terminal.RGB{R: 255, G: 200, B: 100},
		MidHot:    terminal.RGB{R: 255, G: 230, B: 140},
		MidEmber:  terminal.RGB{R: 255, G: 140, B: 50},
		EdgeHot:   terminal.RGB{R: 255, G: 180, B: 80},
		EdgeEmber: terminal.RGB{R: 220, G: 100, B: 40},
		RingColor: terminal.RGB{R: 50, G: 40, B: 60},
	},
	{
		Name:      "Lava",
		CoreHot:   terminal.RGB{R: 255, G: 255, B: 240},
		CoreEmber: terminal.RGB{R: 255, G: 160, B: 60},
		MidHot:    terminal.RGB{R: 255, G: 200, B: 100},
		MidEmber:  terminal.RGB{R: 240, G: 100, B: 30},
		EdgeHot:   terminal.RGB{R: 255, G: 140, B: 60},
		EdgeEmber: terminal.RGB{R: 200, G: 60, B: 25},
		RingColor: terminal.RGB{R: 60, G: 30, B: 40},
	},
	{
		Name:      "Molten",
		CoreHot:   terminal.RGB{R: 230, G: 245, B: 255},
		CoreEmber: terminal.RGB{R: 255, G: 220, B: 140},
		MidHot:    terminal.RGB{R: 255, G: 255, B: 255},
		MidEmber:  terminal.RGB{R: 255, G: 180, B: 80},
		EdgeHot:   terminal.RGB{R: 255, G: 220, B: 180},
		EdgeEmber: terminal.RGB{R: 220, G: 100, B: 50},
		RingColor: terminal.RGB{R: 40, G: 45, B: 60},
	},
}

// Ring represents one Dyson-sphere style orbiting ring
type Ring struct {
	NormalX, NormalY, NormalZ float64
	Angle                     float64
	Velocity                  float64
	PulsePhase                float64
}

// Ember holds the state of the ember effect
type Ember struct {
	CenterX, CenterY float64
	Time             float64
	Rings            [3]Ring

	// Tunable parameters
	PaletteIdx int
	Intensity  float64 // 1.0 = hot, 0.0 = ember

	// Geometry
	RadiusX float64
	RadiusY float64

	// Jagged edge (sine-wave style)
	JaggedAmp     float64 // Max cell displacement
	JaggedFreq    float64 // Number of teeth around perimeter
	JaggedSpeed   float64 // Temporal animation speed
	JaggedOctave2 float64 // Second octave multiplier
	JaggedOctave3 float64 // Third octave multiplier
	EruptionPower float64 // Spike sharpness

	// Glow layers
	CoreFalloff   float64 // Higher = sharper core falloff
	CorePower     float64 // Exponential power
	MidFalloff    float64
	MidPower      float64
	MidIntensity  float64
	EdgePower     float64 // Corona falloff exponent
	EdgeIntensity float64

	// Turbulence
	TurbAmp   float64
	TurbSpeed float64

	// Rings
	RingAlpha   float64
	RingWidth   float64
	RingVisible float64
	RingSpeed   float64
}

func newEmber(screenW, screenH int) *Ember {
	e := &Ember{
		CenterX:    float64(screenW) / 2,
		CenterY:    float64(screenH) / 2,
		Intensity:  1.0,
		PaletteIdx: 0,

		RadiusX: 11.0,
		RadiusY: 5.5,

		JaggedAmp:     1.5,
		JaggedFreq:    12.0,
		JaggedSpeed:   2.5,
		JaggedOctave2: 0.35,
		JaggedOctave3: 0.20,
		EruptionPower: 6.0,

		CoreFalloff:   1.6,
		CorePower:     1.3,
		MidFalloff:    1.0,
		MidPower:      0.6,
		MidIntensity:  0.85,
		EdgePower:     0.4,
		EdgeIntensity: 0.7,

		TurbAmp:   0.12,
		TurbSpeed: 5.0,

		RingAlpha:   0.15,
		RingWidth:   0.06,
		RingVisible: 0.70,
		RingSpeed:   1.0,
	}

	for i := 0; i < 3; i++ {
		tilt := (float64(i) + 0.5) * (math.Pi / 3.5)
		azimuth := float64(i) * (2.0 * math.Pi / 3)

		e.Rings[i] = Ring{
			NormalX:    math.Sin(tilt) * math.Cos(azimuth),
			NormalY:    math.Sin(tilt) * math.Sin(azimuth) / aspectRatio,
			NormalZ:    math.Cos(tilt),
			Angle:      float64(i) * (2.0 * math.Pi / 3),
			Velocity:   1.0 + 0.3*float64(i),
			PulsePhase: float64(i) * 0.7,
		}
	}

	return e
}

func (e *Ember) update(dt float64, screenW, screenH int) {
	e.Time += dt
	e.CenterX = float64(screenW) / 2
	e.CenterY = float64(screenH) / 2

	for i := range e.Rings {
		e.Rings[i].Angle += e.Rings[i].Velocity * e.RingSpeed * dt
	}
}

// getJaggedRadius returns radius at angle theta with multi-octave sine noise
func (e *Ember) getJaggedRadius(theta, rx, ry float64) (float64, float64) {
	// Multi-octave noise for organic jaggedness
	noise := 0.0
	noise += math.Sin(theta*e.JaggedFreq+e.Time*e.JaggedSpeed) * 0.5
	noise += math.Sin(theta*e.JaggedFreq*2.1+e.Time*e.JaggedSpeed*1.3) * e.JaggedOctave2
	noise += math.Sin(theta*e.JaggedFreq*0.5+e.Time*e.JaggedSpeed*0.7) * e.JaggedOctave3

	// Occasional eruption spikes
	eruption := math.Pow(math.Max(0, math.Sin(theta*3.0+e.Time*1.5)), e.EruptionPower) * 1.2

	displacement := (noise + eruption) * e.JaggedAmp

	return rx + displacement, ry + displacement/aspectRatio
}

func lerpRGB(a, b terminal.RGB, t float64) terminal.RGB {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return terminal.RGB{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
	}
}

func renderEmber(e *Ember, cells []terminal.Cell, w, h int) {
	pal := palettes[e.PaletteIdx]
	emberT := 1.0 - e.Intensity

	coreColor := lerpRGB(pal.CoreHot, pal.CoreEmber, emberT)
	midColor := lerpRGB(pal.MidHot, pal.MidEmber, emberT)
	edgeColor := lerpRGB(pal.EdgeHot, pal.EdgeEmber, emberT)

	maxR := e.RadiusX + e.JaggedAmp*2 + 4
	minX := int(e.CenterX - maxR)
	maxX := int(e.CenterX + maxR + 1)
	minY := int(e.CenterY - maxR/aspectRatio - 1)
	maxY := int(e.CenterY + maxR/aspectRatio + 1)

	if minX < 0 {
		minX = 0
	}
	if maxX > w {
		maxX = w
	}
	if minY < 0 {
		minY = 0
	}
	if maxY > h {
		maxY = h
	}

	for y := minY; y < maxY; y++ {
		dy := (float64(y) - e.CenterY) * aspectRatio
		rowOff := y * w

		for x := minX; x < maxX; x++ {
			dx := float64(x) - e.CenterX
			theta := math.Atan2(dy, dx)

			jaggedRX, jaggedRY := e.getJaggedRadius(theta, e.RadiusX, e.RadiusY)

			distSq := (dx*dx)/(jaggedRX*jaggedRX) + (dy*dy)/(jaggedRY*jaggedRY*aspectRatio*aspectRatio)

			if distSq > 1.5 {
				continue
			}

			idx := rowOff + x
			bg := cells[idx].Bg

			normDist := math.Sqrt(distSq)
			if normDist > 1.0 {
				normDist = 1.0
			}

			// Core: sharp bright center
			coreInt := math.Max(0, 1.0-normDist*e.CoreFalloff)
			coreInt = math.Pow(coreInt, e.CorePower)

			// Mid layer: softer glow
			midInt := math.Max(0, 1.0-normDist*e.MidFalloff)
			midInt = math.Pow(midInt, e.MidPower) * e.MidIntensity

			// Edge corona: bright at edges
			coronaInt := math.Pow(1.0-normDist, e.EdgePower) * e.EdgeIntensity

			// Turbulence flicker
			flicker := 1.0 + e.TurbAmp*math.Sin(normDist*12-e.Time*e.TurbSpeed)
			flicker += e.TurbAmp * 0.6 * math.Sin(theta*6+e.Time*e.TurbSpeed*0.6)

			result := bg

			// Corona layer
			if coronaInt > 0.01 {
				coronaCol := render.Scale(edgeColor, coronaInt*flicker)
				result = render.Add(result, coronaCol, 1.0)
			}

			// Mid layer
			if midInt > 0.01 {
				midCol := render.Scale(midColor, midInt*flicker)
				result = render.Screen(result, midCol, 1.0)
			}

			// Core layer
			if coreInt > 0.01 {
				coreCol := render.Scale(coreColor, coreInt*flicker)
				result = render.Add(result, coreCol, 1.0)
			}

			// Rings
			if normDist < e.RingVisible && e.RingAlpha > 0.01 {
				ringVis := renderRings(e, dx, dy, normDist)
				if ringVis > 0.01 {
					ringCol := render.Scale(pal.RingColor, ringVis)
					result = render.Overlay(result, ringCol, ringVis*0.7)
				}
			}

			cells[idx].Bg = result
		}
	}
}

func renderRings(e *Ember, dx, dy, normDist float64) float64 {
	edgeFade := 1.0 - math.Pow(normDist/e.RingVisible, 2)
	totalVis := 0.0

	for i := range e.Rings {
		r := &e.Rings[i]

		cosA := math.Cos(r.Angle)
		sinA := math.Sin(r.Angle)

		dz := math.Sqrt(math.Max(0, 1.0-normDist*normDist))

		rz := dx*sinA*r.NormalX + dy*sinA*r.NormalY + dz*cosA*r.NormalZ

		ringDist := math.Abs(rz)
		vis := math.Exp(-ringDist*ringDist/(e.RingWidth*e.RingWidth)) * edgeFade

		pulse := e.RingAlpha + 0.05*math.Sin(e.Time*1.8+r.PulsePhase)
		vis *= pulse

		if rz < -0.1 {
			vis *= 0.25
		}

		totalVis = math.Max(totalVis, vis)
	}

	return totalVis
}

type star struct {
	x, y, brightness, phase float64
}

func renderStars(stars []star, cells []terminal.Cell, w, h int, t float64) {
	for _, s := range stars {
		sx, sy := int(s.x), int(s.y)
		if sx < 0 || sx >= w || sy < 0 || sy >= h {
			continue
		}
		brite := s.brightness * (0.6 + 0.4*math.Sin(t*3.5+s.phase))
		val := uint8(160 * brite)
		idx := sy*w + sx
		cells[idx].Bg = render.Add(cells[idx].Bg, terminal.RGB{R: val, G: val, B: val}, 1.0)
	}
}

// Control represents a tunable parameter
type Control struct {
	Name   string
	Value  *float64
	Min    float64
	Max    float64
	Step   float64
	IntVal *int // For integer controls
	IntMax int
}

func renderHUD(cells []terminal.Cell, w, h int, e *Ember, controls []Control, selected int) {
	pal := palettes[e.PaletteIdx]

	fg := terminal.RGB{R: 180, G: 180, B: 180}
	fgSel := terminal.RGB{R: 255, G: 255, B: 100}
	fgVal := terminal.RGB{R: 100, G: 220, B: 150}

	lines := make([]struct {
		text string
		sel  bool
	}, 0, len(controls)+3)

	lines = append(lines, struct {
		text string
		sel  bool
	}{
		fmt.Sprintf("=== EMBER SANDBOX === Palette: %s", pal.Name), false,
	})
	lines = append(lines, struct {
		text string
		sel  bool
	}{
		"[W/S] Navigate  [A/D] Adjust  [1/2/3] Palette  [Q] Quit", false,
	})
	lines = append(lines, struct {
		text string
		sel  bool
	}{"", false})

	for i, c := range controls {
		var val string
		if c.IntVal != nil {
			val = fmt.Sprintf("%d", *c.IntVal)
		} else {
			val = fmt.Sprintf("%.2f", *c.Value)
		}
		marker := "  "
		if i == selected {
			marker = "> "
		}
		lines = append(lines, struct {
			text string
			sel  bool
		}{
			fmt.Sprintf("%s%-14s %s", marker, c.Name, val),
			i == selected,
		})
	}

	startY := h - len(lines) - 1

	for i, line := range lines {
		y := startY + i
		if y < 0 || y >= h {
			continue
		}

		color := fg
		if line.sel {
			color = fgSel
		}

		// Find value position for coloring
		valStart := -1
		for j := len(line.text) - 1; j >= 0; j-- {
			r := line.text[j]
			if r == ' ' && valStart == -1 {
				continue
			}
			if r == ' ' {
				valStart = j + 1
				break
			}
		}

		for x, r := range line.text {
			if x >= w {
				break
			}
			idx := y*w + x
			cells[idx].Rune = r
			if x >= valStart && valStart > 0 && line.sel {
				cells[idx].Fg = fgVal
			} else {
				cells[idx].Fg = color
			}
		}
	}
}

func startInputReader(term terminal.Terminal) <-chan terminal.Event {
	ch := make(chan terminal.Event, 64)
	go func() {
		defer close(ch)
		for {
			ev := term.PollEvent()
			if ev.Type != terminal.EventKey {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			ch <- ev
		}
	}()
	return ch
}

func main() {
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal init: %v\n", err)
		os.Exit(1)
	}
	defer term.Fini()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Fini()
		os.Exit(0)
	}()

	w, h := term.Size()
	cells := make([]terminal.Cell, w*h)
	bgColor := terminal.RGB{R: 12, G: 12, B: 20}

	stars := make([]star, 60)
	for i := range stars {
		stars[i] = star{
			x:          rand.Float64() * float64(w),
			y:          rand.Float64() * float64(h),
			brightness: 0.3 + rand.Float64()*0.7,
			phase:      rand.Float64() * math.Pi * 2,
		}
	}

	ember := newEmber(w, h)

	// Build controls list
	controls := []Control{
		{"Intensity", &ember.Intensity, 0.0, 1.0, 0.05, nil, 0},
		{"RadiusX", &ember.RadiusX, 5.0, 25.0, 0.5, nil, 0},
		{"RadiusY", &ember.RadiusY, 2.0, 12.0, 0.25, nil, 0},
		{"JaggedAmp", &ember.JaggedAmp, 0.0, 4.0, 0.1, nil, 0},
		{"JaggedFreq", &ember.JaggedFreq, 4.0, 24.0, 1.0, nil, 0},
		{"JaggedSpeed", &ember.JaggedSpeed, 0.5, 6.0, 0.25, nil, 0},
		{"Octave2", &ember.JaggedOctave2, 0.0, 1.0, 0.05, nil, 0},
		{"Octave3", &ember.JaggedOctave3, 0.0, 1.0, 0.05, nil, 0},
		{"EruptionPow", &ember.EruptionPower, 1.0, 12.0, 0.5, nil, 0},
		{"CoreFalloff", &ember.CoreFalloff, 0.5, 3.0, 0.1, nil, 0},
		{"CorePower", &ember.CorePower, 0.5, 3.0, 0.1, nil, 0},
		{"MidFalloff", &ember.MidFalloff, 0.3, 2.0, 0.1, nil, 0},
		{"MidPower", &ember.MidPower, 0.2, 2.0, 0.1, nil, 0},
		{"MidIntensity", &ember.MidIntensity, 0.2, 1.5, 0.05, nil, 0},
		{"EdgePower", &ember.EdgePower, 0.1, 1.5, 0.05, nil, 0},
		{"EdgeIntensity", &ember.EdgeIntensity, 0.2, 1.5, 0.05, nil, 0},
		{"TurbAmp", &ember.TurbAmp, 0.0, 0.4, 0.02, nil, 0},
		{"TurbSpeed", &ember.TurbSpeed, 1.0, 12.0, 0.5, nil, 0},
		{"RingAlpha", &ember.RingAlpha, 0.0, 0.5, 0.02, nil, 0},
		{"RingWidth", &ember.RingWidth, 0.02, 0.2, 0.01, nil, 0},
		{"RingVisible", &ember.RingVisible, 0.3, 1.0, 0.05, nil, 0},
		{"RingSpeed", &ember.RingSpeed, 0.2, 3.0, 0.1, nil, 0},
	}

	selected := 0

	inputCh := startInputReader(term)
	lastFrame := time.Now()
	running := true

	for running {
		frameStart := time.Now()
		dt := frameStart.Sub(lastFrame).Seconds()
		lastFrame = frameStart

	drainInput:
		for {
			select {
			case ev, ok := <-inputCh:
				if !ok {
					running = false
					break drainInput
				}
				if ev.Type == terminal.EventResize {
					w, h = term.Size()
					cells = make([]terminal.Cell, w*h)
					for i := range stars {
						stars[i].x = rand.Float64() * float64(w)
						stars[i].y = rand.Float64() * float64(h)
					}
					continue
				}
				switch {
				case ev.Key == terminal.KeyRune && ev.Rune == 'q':
					running = false
				case ev.Key == terminal.KeyRune && ev.Rune == '1':
					ember.PaletteIdx = 0
				case ev.Key == terminal.KeyRune && ev.Rune == '2':
					ember.PaletteIdx = 1
				case ev.Key == terminal.KeyRune && ev.Rune == '3':
					ember.PaletteIdx = 2
				case ev.Key == terminal.KeyRune && (ev.Rune == 'w' || ev.Rune == 'W'):
					selected--
					if selected < 0 {
						selected = len(controls) - 1
					}
				case ev.Key == terminal.KeyRune && (ev.Rune == 's' || ev.Rune == 'S'):
					selected++
					if selected >= len(controls) {
						selected = 0
					}
				case ev.Key == terminal.KeyUp:
					selected--
					if selected < 0 {
						selected = len(controls) - 1
					}
				case ev.Key == terminal.KeyDown:
					selected++
					if selected >= len(controls) {
						selected = 0
					}
				case ev.Key == terminal.KeyRune && (ev.Rune == 'a' || ev.Rune == 'A'), ev.Key == terminal.KeyLeft:
					c := &controls[selected]
					if c.IntVal != nil {
						*c.IntVal--
						if *c.IntVal < 0 {
							*c.IntVal = 0
						}
					} else {
						*c.Value -= c.Step
						if *c.Value < c.Min {
							*c.Value = c.Min
						}
					}
				case ev.Key == terminal.KeyRune && (ev.Rune == 'd' || ev.Rune == 'D'), ev.Key == terminal.KeyRight:
					c := &controls[selected]
					if c.IntVal != nil {
						*c.IntVal++
						if *c.IntVal > c.IntMax {
							*c.IntVal = c.IntMax
						}
					} else {
						*c.Value += c.Step
						if *c.Value > c.Max {
							*c.Value = c.Max
						}
					}
				}
			default:
				break drainInput
			}
		}

		ember.update(dt, w, h)

		for i := range cells {
			cells[i] = terminal.Cell{Rune: ' ', Bg: bgColor}
		}

		renderStars(stars, cells, w, h, ember.Time)
		renderEmber(ember, cells, w, h)
		renderHUD(cells, w, h, ember, controls, selected)

		term.Flush(cells, w, h)

		elapsed := time.Since(frameStart)
		if elapsed < 16*time.Millisecond {
			time.Sleep(16*time.Millisecond - elapsed)
		}
	}

	term.Fini()
}