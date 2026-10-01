package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// --- Lighting & Math Precomputation ---
var (
	lightX, lightY, lightZ float64
	halfX, halfY, halfZ    float64
)

func initLighting() {
	lx, ly, lz := -0.4, -0.6, 0.8
	m := math.Sqrt(lx*lx + ly*ly + lz*lz)
	lightX, lightY, lightZ = lx/m, ly/m, lz/m

	hx, hy, hz := lightX, lightY, lightZ+1.0
	m = math.Sqrt(hx*hx + hy*hy + hz*hz)
	halfX, halfY, halfZ = hx/m, hy/m, hz/m
}

// --- Kraken Types and States ---

const (
	StateIdle = iota
	StateAttack
	StateMove
	StateSpin
)

type Kraken struct {
	X, Y             float64
	TargetX, TargetY float64
	Angle            float64
	Time             float64

	State       int
	StateTimer  float64
	AttackLegs  int
	FreezeState bool

	// Direction Tracking for physics
	DirX, DirY float64
	TurnDir    float64

	// Smooth Transition State
	CurrentRotSpeed float64
	TargetRotSpeed  float64
	MoveBlend       float64
	AttackT         float64

	// Tuning Controls
	BodyRadius   float64
	BodyProfile  float64 // 0: Dome, 1: Void, 2: Dark Core, 3: Lava Lamp
	LegLength    float64
	LegThickness float64
	AttackReach  float64
	MoveReach    float64
	WiggleAmp    float64
	WiggleFreq   float64
	WiggleSpeed  float64
	BaseRotSpeed float64
	MoveSpeed    float64
	InertiaBend  float64
	InertiaCurve float64

	// Colors
	BodyColor    color.RGB
	RimColor     color.RGB
	LegBaseColor color.RGB
	LegTipColor  color.RGB
	AttackColor  color.RGB
}

func newKraken(w, h int) *Kraken {
	return &Kraken{
		X: float64(w) / 2,
		Y: float64(h) / 2,

		State:      StateIdle,
		StateTimer: 2.0,

		DirX:    1.0,
		DirY:    0.0,
		TurnDir: 1.0,

		// User Defaults
		BodyRadius:   4.00,
		BodyProfile:  1.0,
		LegLength:    30.00,
		LegThickness: 3.40,
		AttackReach:  40.00,
		MoveReach:    12.00,
		WiggleAmp:    0.20,
		WiggleFreq:   0.20,
		WiggleSpeed:  4.00,
		BaseRotSpeed: 0.20,
		MoveSpeed:    60.00,
		InertiaBend:  0.10,
		InertiaCurve: 0.10,

		CurrentRotSpeed: 0.20,
		TargetRotSpeed:  0.20,

		BodyColor:    color.RGB{R: 20, G: 5, B: 30},
		RimColor:     color.RGB{R: 120, G: 30, B: 180},
		LegBaseColor: color.RGB{R: 35, G: 10, B: 50},
		LegTipColor:  color.RGB{R: 80, G: 15, B: 90},
		AttackColor:  color.RGB{R: 255, G: 30, B: 60},
	}
}

func (k *Kraken) update(dt float64, w, h int) {
	k.Time += dt

	if !k.FreezeState {
		k.StateTimer -= dt

		if k.StateTimer <= 0 {
			r := rand.Float64()
			if r < 0.30 {
				k.State = StateAttack
				k.StateTimer = 1.5
				k.AttackLegs = rand.Intn(2)
			} else if r < 0.60 {
				k.State = StateMove
				k.StateTimer = 3.0
				k.TargetX = float64(w)*0.2 + rand.Float64()*float64(w)*0.6
				k.TargetY = float64(h)*0.2 + rand.Float64()*float64(h)*0.6

				// Calculate rotational TurnDir based on movement cross product
				dx := k.TargetX - k.X
				dy := k.TargetY - k.Y
				dist := math.Hypot(dx, dy)
				if dist > 0.001 {
					ndx, ndy := dx/dist, dy/dist
					cross := k.DirX*ndy - k.DirY*ndx
					if cross >= 0 {
						k.TurnDir = 1.0
					} else {
						k.TurnDir = -1.0
					}
					k.DirX, k.DirY = ndx, ndy
				}
			} else if r < 0.75 {
				k.State = StateSpin
				k.StateTimer = 2.0
				k.TurnDir = 1.0
				if rand.Intn(2) == 0 {
					k.TurnDir = -1.0
				}
			} else {
				k.State = StateIdle
				k.StateTimer = 1.0 + rand.Float64()*2.0
			}
		}
	}

	// Dynamic Target Rotation Logic
	if k.State == StateAttack {
		k.TargetRotSpeed = 0.0 // Brace for attack
	} else if k.State == StateIdle {
		k.TargetRotSpeed = k.BaseRotSpeed // Revert to base idle rotation
	} else if k.State == StateMove {
		dx := k.TargetX - k.X
		dy := k.TargetY - k.Y
		dist := math.Hypot(dx, dy*2.0)
		if dist > k.MoveSpeed*dt {
			k.X += (dx / dist) * k.MoveSpeed * dt
			k.Y += (dy / dist) * (k.MoveSpeed / 2.0) * dt
		} else if !k.FreezeState {
			k.StateTimer = 0
		}
		// Fast spin during movement aligned with travel direction
		k.TargetRotSpeed = k.TurnDir * (math.Abs(k.BaseRotSpeed) + 0.5)
	} else if k.State == StateSpin {
		k.TargetRotSpeed = k.TurnDir * (math.Abs(k.BaseRotSpeed) + 0.6)
	}

	if k.State == StateIdle {
		cx, cy := float64(w)/2, float64(h)/2
		k.X += (cx - k.X) * 0.2 * dt
		k.Y += (cy - k.Y) * 0.2 * dt
	}

	// Smooth Lerping for Dynamics
	k.CurrentRotSpeed += (k.TargetRotSpeed - k.CurrentRotSpeed) * dt * 4.0
	k.Angle += k.CurrentRotSpeed * dt

	// Blend states
	targetMoveBlend := 0.0
	if k.State == StateMove {
		targetMoveBlend = 1.0
	}
	k.MoveBlend += (targetMoveBlend - k.MoveBlend) * dt * 4.0

	if k.State == StateAttack {
		t := 1.0 - (k.StateTimer / 1.5)
		k.AttackT = math.Sin(t * math.Pi)
		if k.AttackT < 0 {
			k.AttackT = 0
		}
	} else {
		k.AttackT -= dt * 5.0
		if k.AttackT < 0 {
			k.AttackT = 0
		}
	}
}

// --- Rendering ---

func lerpRGB(a, b color.RGB, t float64) color.RGB {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGB{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
	}
}

func clampF(v float64) uint8 {
	if v > 255.0 {
		return 255
	}
	if v < 0.0 {
		return 0
	}
	return uint8(v)
}

func getEdgeRune(fraction float64) rune {
	switch {
	case fraction < 0.25:
		return '▓'
	case fraction < 0.55:
		return '▒'
	case fraction < 0.85:
		return '░'
	default:
		return ' '
	}
}

func drawTentacleCircle(cells []terminal.Cell, w, h int, cx, cy, radius, step float64, isAttack bool, k *Kraken) {
	radSq := radius * radius
	minX := int(cx - radius*2 - 2)
	maxX := int(cx + radius*2 + 2)
	minY := int(cy - radius - 2)
	maxY := int(cy + radius + 2)

	attackHeat := step * step * k.AttackT
	moveHeat := step * step * k.MoveBlend * 0.4
	heat := math.Max(attackHeat, moveHeat)
	if !isAttack {
		heat = moveHeat
	}

	col := lerpRGB(k.LegBaseColor, k.LegTipColor, step)
	col = lerpRGB(col, k.AttackColor, heat)

	bgColor := color.RGB{R: 12, G: 12, B: 18}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if x < 0 || x >= w || y < 0 || y >= h {
				continue
			}

			dx := float64(x) - cx
			dy := (float64(y) - cy) * 2.0
			distSq := dx*dx + dy*dy

			idx := y*w + x

			if distSq <= radSq {
				cells[idx].Bg = col
				cells[idx].Rune = ' '
				cells[idx].Fg = col
			} else {
				dist := math.Sqrt(distSq)
				diff := dist - radius
				if diff <= 1.8 {
					fraction := diff / 1.8
					r := getEdgeRune(fraction)
					if r != ' ' {
						if cells[idx].Rune == ' ' && cells[idx].Bg == bgColor {
							cells[idx].Fg = col
							cells[idx].Rune = r
						}
					}
				}
			}
		}
	}
}

func renderTentacles(k *Kraken, cells []terminal.Cell, w, h int) {
	for i := 0; i < 8; i++ {
		isAttackingLeg := (k.State == StateAttack) && (i%2 == k.AttackLegs)
		baseTheta := k.Angle + float64(i)*(math.Pi/4.0)

		attackExt := 0.0
		if isAttackingLeg {
			attackExt = k.AttackT
		}

		currentLen := k.LegLength + attackExt*k.AttackReach + k.MoveBlend*k.MoveReach

		dr := 0.35
		for r := 0.0; r <= currentLen; r += dr {
			step := r / currentLen

			// Directional physics-based drag
			drag := -k.CurrentRotSpeed * k.InertiaBend * 15.0 * math.Pow(r/10.0, k.InertiaCurve)

			wiggleAmp := k.WiggleAmp * (1.0 - k.MoveBlend*0.7) * (1.0 - attackExt*0.8)
			wiggle := math.Sin(r*k.WiggleFreq-k.Time*k.WiggleSpeed) * wiggleAmp

			theta := baseTheta + drag + wiggle

			cx := k.X + r*math.Cos(theta)
			cy := k.Y + r*math.Sin(theta)/2.0

			radius := k.LegThickness * math.Pow(1.0-step, 0.7)

			drawTentacleCircle(cells, w, h, cx, cy, radius, step, isAttackingLeg, k)
		}
	}
}

func renderKrakenBody(k *Kraken, cells []terminal.Cell, w, h int) {
	rY := k.BodyRadius
	rX := rY * 2.0 // Aspect ratio correction

	minX := max(0, int(k.X-rX-2))
	maxX := min(w-1, int(k.X+rX+2))
	minY := max(0, int(k.Y-rY-2))
	maxY := min(h-1, int(k.Y+rY+2))

	bgColor := color.RGB{R: 12, G: 12, B: 18}
	profile := int(k.BodyProfile)

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			dx := (float64(x) - k.X) / rX
			dy := (float64(y) - k.Y) / rY
			distSq := dx*dx + dy*dy

			if distSq > 1.0 {
				continue
			}

			idx := y*w + x
			if idx < 0 || idx >= len(cells) {
				continue
			}

			dist := math.Sqrt(distSq)
			nz := math.Sqrt(1.0 - distSq)

			var litColor color.RGB

			switch profile {
			case 0: // Lit Dome
				spec := dx*halfX + dy*halfY + nz*halfZ
				if spec < 0 {
					spec = 0
				}
				spec = math.Pow(spec, 14.0) * 0.7

				diff := dx*lightX + dy*lightY + nz*lightZ
				if diff < 0 {
					diff = 0
				}

				intensity := 0.3 + diff*0.7
				fr := float64(k.BodyColor.R)*intensity + spec*255
				fg := float64(k.BodyColor.G)*intensity + spec*255
				fb := float64(k.BodyColor.B)*intensity + spec*255

				rim := 1.0 - nz
				rim = math.Pow(rim, 2.5) * 0.5
				fr += float64(k.RimColor.R) * rim
				fg += float64(k.RimColor.G) * rim
				fb += float64(k.RimColor.B) * rim
				litColor = color.RGB{R: clampF(fr), G: clampF(fg), B: clampF(fb)}

			case 1: // Void Hole
				litColor = bgColor

			case 2: // Dark Core
				litColor = color.RGB{R: 8, G: 4, B: 12} // Very dark purple/black

			case 3: // Lava Lamp
				px := dx * 3.5
				py := dy * 3.5
				pattern := math.Sin(px+k.Time*1.5)*math.Cos(py-k.Time*1.1) + math.Sin((px+py)*1.8+k.Time*0.8)
				pattern = (pattern + 2.0) / 4.0
				if pattern < 0 {
					pattern = 0
				}
				litColor = lerpRGB(k.BodyColor, k.RimColor, pattern*1.2)
			}

			// Smoothstep alpha blend: 1.0 at center, perfectly fades to leg base at edge
			alpha := 1.0 - dist
			alpha = alpha * alpha * (3.0 - 2.0*alpha)

			cells[idx].Bg = lerpRGB(cells[idx].Bg, litColor, alpha)

			// Erase edge char artifacts under the body
			if alpha > 0.3 {
				cells[idx].Rune = ' '
			}
		}
	}
}

// --- Control & HUD System ---

type Control struct {
	Name  string
	Value *float64
	Min   float64
	Max   float64
	Step  float64
}

func renderHUD(cells []terminal.Cell, w, h int, k *Kraken, controls []Control, selected int) {
	fg := color.RGB{R: 160, G: 160, B: 170}
	fgSel := color.RGB{R: 255, G: 220, B: 100}
	fgVal := color.RGB{R: 100, G: 255, B: 180}
	bg := color.RGB{R: 20, G: 20, B: 30}

	stateStr := "IDLE"
	stateCol := fg
	if k.State == StateAttack {
		stateStr = "ATTACK"
		stateCol = k.AttackColor
	} else if k.State == StateMove {
		stateStr = "MOVE"
		stateCol = color.RGB{R: 100, G: 200, B: 255}
	} else if k.State == StateSpin {
		stateStr = "SPIN"
		stateCol = color.RGB{R: 200, G: 100, B: 255}
	}

	if k.FreezeState {
		stateStr += " [FROZEN]"
	}

	lines := []struct {
		text string
		sel  bool
	}{
		{fmt.Sprintf("=== KRAKEN SANDBOX ===  State: [%s]", stateStr), false},
		{"[W/S] Nav  [A/D] Adj  [Spc] Attack  [Ent] Move  [R] Spin  [F] Freeze  [Q] Quit", false},
		{"", false},
	}

	for i, c := range controls {
		val := fmt.Sprintf("%6.2f", *c.Value)
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

		col := fg
		if line.sel {
			col = fgSel
		}

		valStart := -1
		for j := len(line.text) - 1; j >= 0; j-- {
			if line.text[j] == ' ' && valStart == -1 {
				continue
			}
			if line.text[j] == ' ' {
				valStart = j + 1
				break
			}
		}

		stateIdx := -1
		if i == 0 {
			for j := 0; j < len(line.text); j++ {
				if line.text[j] == '[' {
					stateIdx = j + 1
					break
				}
			}
		}

		for x, r := range line.text {
			if x >= w {
				break
			}
			idx := y*w + x
			cells[idx].Rune = r
			cells[idx].Bg = bg
			if i == 0 && stateIdx != -1 && x >= stateIdx && x < len(line.text) {
				cells[idx].Fg = stateCol
			} else if x >= valStart && valStart > 0 && line.sel {
				cells[idx].Fg = fgVal
			} else {
				cells[idx].Fg = col
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
			if ev.Type != terminal.EventKey && ev.Type != terminal.EventResize {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			ch <- ev
		}
	}()
	return ch
}

func main() {
	initLighting()

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
	bgColor := color.RGB{R: 12, G: 12, B: 18}

	kraken := newKraken(w, h)

	controls := []Control{
		{"BodyRadius", &kraken.BodyRadius, 1.0, 25.0, 0.5},
		{"BodyProfile", &kraken.BodyProfile, 0.0, 3.0, 1.0},
		{"LegLength", &kraken.LegLength, 5.0, 80.0, 1.0},
		{"LegThickness", &kraken.LegThickness, 1.0, 12.0, 0.2},
		{"AttackReach", &kraken.AttackReach, 0.0, 80.0, 1.0},
		{"MoveReach", &kraken.MoveReach, 0.0, 40.0, 1.0},
		{"WiggleAmp", &kraken.WiggleAmp, 0.0, 2.0, 0.05},
		{"WiggleFreq", &kraken.WiggleFreq, 0.05, 3.0, 0.05},
		{"WiggleSpeed", &kraken.WiggleSpeed, 0.0, 20.0, 0.5},
		{"BaseRotSpeed", &kraken.BaseRotSpeed, -1.0, 1.0, 0.02},
		{"MoveSpeed", &kraken.MoveSpeed, 5.0, 120.0, 1.0},
		{"InertiaBend", &kraken.InertiaBend, 0.0, 2.0, 0.05},
		{"InertiaCurve", &kraken.InertiaCurve, 0.1, 3.0, 0.1},
	}

	selected := 0
	inputCh := startInputReader(term)
	lastFrame := time.Now()
	running := true

	for running {
		frameStart := time.Now()
		dt := frameStart.Sub(lastFrame).Seconds()
		lastFrame = frameStart
		if dt > 0.1 {
			dt = 0.1
		}

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
					kraken.X = float64(w) / 2
					kraken.Y = float64(h) / 2
					continue
				}
				if ev.Type == terminal.EventKey {
					switch {
					case ev.Key == terminal.KeyRune && ev.Rune == 'q':
						running = false
					case ev.Key == terminal.KeyRune && ev.Rune == ' ':
						kraken.State = StateAttack
						kraken.StateTimer = 1.5
						kraken.AttackLegs = rand.Intn(2)
					case ev.Key == terminal.KeyEnter:
						kraken.State = StateMove
						kraken.StateTimer = 3.0

						// Assign random target
						kraken.TargetX = float64(w)*0.2 + rand.Float64()*float64(w)*0.6
						kraken.TargetY = float64(h)*0.2 + rand.Float64()*float64(h)*0.6

						// Pre-calculate spin dir
						dx := kraken.TargetX - kraken.X
						dy := kraken.TargetY - kraken.Y
						dist := math.Hypot(dx, dy)
						if dist > 0.001 {
							ndx, ndy := dx/dist, dy/dist
							if kraken.DirX*ndy-kraken.DirY*ndx >= 0 {
								kraken.TurnDir = 1.0
							} else {
								kraken.TurnDir = -1.0
							}
							kraken.DirX, kraken.DirY = ndx, ndy
						}

					case ev.Key == terminal.KeyRune && (ev.Rune == 'r' || ev.Rune == 'R'):
						kraken.State = StateSpin
						kraken.StateTimer = 2.0
						kraken.TurnDir = 1.0
						if rand.Intn(2) == 0 {
							kraken.TurnDir = -1.0
						}
					case ev.Key == terminal.KeyRune && (ev.Rune == 'f' || ev.Rune == 'F'):
						kraken.FreezeState = !kraken.FreezeState
					case ev.Key == terminal.KeyRune && (ev.Rune == 'w' || ev.Rune == 'W'), ev.Key == terminal.KeyUp:
						selected--
						if selected < 0 {
							selected = len(controls) - 1
						}
					case ev.Key == terminal.KeyRune && (ev.Rune == 's' || ev.Rune == 'S'), ev.Key == terminal.KeyDown:
						selected++
						if selected >= len(controls) {
							selected = 0
						}
					case ev.Key == terminal.KeyRune && (ev.Rune == 'a' || ev.Rune == 'A'), ev.Key == terminal.KeyLeft:
						c := &controls[selected]
						*c.Value -= c.Step
						if math.Abs(*c.Value) < 0.0001 {
							*c.Value = 0.0
						}
						if *c.Value < c.Min {
							*c.Value = c.Min
						}
					case ev.Key == terminal.KeyRune && (ev.Rune == 'd' || ev.Rune == 'D'), ev.Key == terminal.KeyRight:
						c := &controls[selected]
						*c.Value += c.Step
						if math.Abs(*c.Value) < 0.0001 {
							*c.Value = 0.0
						}
						if *c.Value > c.Max {
							*c.Value = c.Max
						}
					}
				}
			default:
				break drainInput
			}
		}

		kraken.update(dt, w, h)

		for i := range cells {
			cells[i] = terminal.Cell{Rune: ' ', Bg: bgColor}
		}

		renderTentacles(kraken, cells, w, h)
		renderKrakenBody(kraken, cells, w, h)

		renderHUD(cells, w, h, kraken, controls, selected)

		term.Flush(cells, w, h)

		elapsed := time.Since(frameStart)
		if elapsed < 16*time.Millisecond {
			time.Sleep(16*time.Millisecond - elapsed)
		}
	}
}
