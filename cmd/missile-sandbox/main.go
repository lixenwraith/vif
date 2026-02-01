package main

import (
	"fmt"
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// --- Visual Constants ---
var (
	// Tokyo Night-ish palette
	ColorBg    = terminal.RGB{R: 26, G: 27, B: 38}
	ColorSmoke = terminal.RGB{R: 100, G: 100, B: 110}
	ColorFire  = terminal.RGB{R: 255, G: 160, B: 50}
	ColorCyan  = terminal.RGB{R: 0, G: 255, B: 255}
	ColorPink  = terminal.RGB{R: 255, G: 0, B: 255}
)

// --- Types ---

type MissileType int

const (
	MissileKinetic MissileType = iota
	MissileHelix
	MissileSeeker
)

type Particle struct {
	X, Y       int64 // Q32.32
	VelX, VelY int64
	Age        int // Frames alive
	MaxAge     int
	Char       rune
	ColorStart terminal.RGB
	ColorEnd   terminal.RGB
}

type Missile struct {
	Type   MissileType
	Active bool
	Pos    core.Kinetic
	Origin core.Point // Screen coords
	Target core.Point // Screen coords

	// State for specific behaviors
	Age       int64 // Frames
	Phase     int64 // For Helix
	SteerVecX int64 // For Seeker smoothing
	SteerVecY int64

	Trail []Particle
}

// Global state for screen dimensions
var (
	screenWidth  int
	screenHeight int
)

// --- Logic Implementation ---

func main() {
	// 1. Setup Terminal
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()
	term.SetCursorVisible(false)

	// 2. Get Initial Size
	screenWidth, screenHeight = term.Size()

	// 3. Setup Render Buffer
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, screenWidth, screenHeight)

	// 4. State
	missiles := make([]*Missile, 0)

	// Targets: Top-Right, Mid-Right, Bottom-Right
	targets := make([]core.Point, 3)
	updateTargets(targets)

	currentTargetIdx := 1
	currentType := MissileKinetic

	// Origin: Mid-Left
	origin := core.Point{X: 10, Y: screenHeight / 2}

	inputCh := make(chan terminal.Event, 10)
	go func() {
		for {
			inputCh <- term.PollEvent()
		}
	}()

	resizeCh := term.ResizeChan()
	ticker := time.NewTicker(time.Second / 60)
	defer ticker.Stop()

	// 5. Main Loop
	running := true
	for running {
		select {
		case ev := <-inputCh:
			if ev.Type == terminal.EventKey {
				switch ev.Key {
				case terminal.KeyEscape, terminal.KeyCtrlC:
					running = false

				// --- Firing Controls ---
				// Handle Spacebar (KeySpace usually triggers on specialized keyboards,
				// but standard typing sends a Rune ' ')
				case terminal.KeySpace:
					m := SpawnMissile(currentType, origin, targets[currentTargetIdx])
					missiles = append(missiles, m)

				case terminal.KeyRune:
					if ev.Rune == ' ' {
						m := SpawnMissile(currentType, origin, targets[currentTargetIdx])
						missiles = append(missiles, m)
					}
					// Support number keys as fallback
					if ev.Rune == '1' {
						currentType = MissileKinetic
					}
					if ev.Rune == '2' {
						currentType = MissileHelix
					}
					if ev.Rune == '3' {
						currentType = MissileSeeker
					}

				// --- Navigation Controls ---
				case terminal.KeyUp:
					currentTargetIdx = (currentTargetIdx - 1 + len(targets)) % len(targets)
				case terminal.KeyDown:
					currentTargetIdx = (currentTargetIdx + 1) % len(targets)

				// --- Weapon Switching (User Mappings) ---
				case terminal.KeyEnter:
					currentType = MissileKinetic
				case terminal.KeyTab:
					currentType = MissileHelix
				case terminal.KeyBackspace:
					currentType = MissileSeeker
				}
			}

		case resize := <-resizeCh:
			screenWidth, screenHeight = resize.Width, resize.Height
			buf.Resize(screenWidth, screenHeight)
			updateTargets(targets)
			origin = core.Point{X: 10, Y: screenHeight / 2}
			term.Sync()

		case <-ticker.C:
			// Update
			UpdateMissiles(missiles)

			// Cleanup dead missiles
			active := missiles[:0]
			for _, m := range missiles {
				if m.Active || len(m.Trail) > 0 {
					active = append(active, m)
				}
			}
			missiles = active

			// Render
			buf.Clear()

			// Draw Targets
			for i, t := range targets {
				char := 'O'
				color := terminal.RGB{R: 100, G: 100, B: 100}
				if i == currentTargetIdx {
					char = 'X'
					color = terminal.RGB{R: 255, G: 0, B: 0}
				}
				if t.X < screenWidth && t.Y < screenHeight {
					buf.Set(t.X, t.Y, char, color, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
				}
			}

			// Draw Origin
			buf.Set(origin.X, origin.Y, '>', terminal.RGB{R: 0, G: 255, B: 0}, ColorBg, render.BlendReplace, 1.0, terminal.AttrNone)

			// Draw UI
			uiText := fmt.Sprintf("Type: %s [Ent/Tab/Bksp] | Target: %d [Up/Down] | Fire: [Space] | Esc: Quit",
				MissileTypeName(currentType), currentTargetIdx)
			DrawString(buf, 2, screenHeight-1, uiText, terminal.RGB{R: 200, G: 200, B: 200})

			// Draw Missiles & Trails
			RenderMissiles(buf, missiles)

			// Output
			buf.FlushToTerminal(term)
		}
	}
}

func updateTargets(targets []core.Point) {
	targets[0] = core.Point{X: screenWidth - 10, Y: 5}
	targets[1] = core.Point{X: screenWidth - 10, Y: screenHeight / 2}
	targets[2] = core.Point{X: screenWidth - 10, Y: screenHeight - 5}
}

// --- Spawning & Updates ---

func SpawnMissile(t MissileType, origin, target core.Point) *Missile {
	m := &Missile{
		Type:   t,
		Active: true,
		Origin: origin,
		Target: target,
		Pos: core.Kinetic{
			PreciseX: vmath.FromInt(origin.X),
			PreciseY: vmath.FromInt(origin.Y),
		},
		Trail: make([]Particle, 0, 50),
	}

	// Initial Velocity Calculation
	dx := vmath.FromInt(target.X - origin.X)
	dy := vmath.FromInt(target.Y - origin.Y)
	dist := vmath.Magnitude(dx, dy)

	if dist == 0 {
		dist = vmath.Scale
	}

	dirX := vmath.Div(dx, dist)
	dirY := vmath.Div(dy, dist)

	switch t {
	case MissileKinetic:
		speed := vmath.FromInt(60) // 60 cells/sec
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed) - vmath.FromInt(10) // Aim slightly up

	case MissileHelix:
		speed := vmath.FromInt(40)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed)

	case MissileSeeker:
		speed := vmath.FromInt(15)
		// Launch perpendicular to target
		perpX, perpY := vmath.Perpendicular(dirX, dirY)

		if m.Origin.Y > m.Target.Y {
			perpX, perpY = -perpX, -perpY
		}

		m.Pos.VelX = vmath.Mul(perpX, speed)
		m.Pos.VelY = vmath.Mul(perpY, speed)
	}

	return m
}

func UpdateMissiles(missiles []*Missile) {
	dt := vmath.FromFloat(1.0 / 60.0)

	for _, m := range missiles {
		if !m.Active {
			UpdateTrail(m)
			continue
		}

		m.Age++

		switch m.Type {
		case MissileKinetic:
			// Ballistic
			gravity := vmath.FromInt(20)
			m.Pos.VelY += vmath.Mul(gravity, dt)

			m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
			m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)

			if m.Age%2 == 0 {
				AddSmokeParticle(m)
			}

		case MissileHelix:
			// Linear
			m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
			m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)
			m.Phase += vmath.FromInt(15)

			// Helix Trail
			baseX, baseY := vmath.Normalize2D(m.Pos.VelX, m.Pos.VelY)
			perpX, perpY := vmath.Perpendicular(baseX, baseY)

			amp := vmath.FromInt(2)
			sinVal := vmath.Sin(m.Phase)
			offX := vmath.Mul(vmath.Mul(perpX, amp), sinVal)
			offY := vmath.Mul(vmath.Mul(perpY, amp), sinVal)

			AddHelixParticle(m, offX, offY, ColorCyan)
			AddHelixParticle(m, -offX, -offY, ColorPink)

		case MissileSeeker:
			// Steering
			targetX := vmath.FromInt(m.Target.X)
			targetY := vmath.FromInt(m.Target.Y)

			dx := targetX - m.Pos.PreciseX
			dy := targetY - m.Pos.PreciseY
			dist := vmath.Magnitude(dx, dy)

			if dist < vmath.FromInt(1) {
				m.Active = false
				continue
			}

			maxSpeed := vmath.FromInt(55)
			steerForce := vmath.FromInt(120)

			desiredX, desiredY := vmath.Normalize2D(dx, dy)
			desiredX = vmath.Mul(desiredX, maxSpeed)
			desiredY = vmath.Mul(desiredY, maxSpeed)

			steerX := desiredX - m.Pos.VelX
			steerY := desiredY - m.Pos.VelY

			steerX, steerY = vmath.ClampMagnitude(steerX, steerY, steerForce)

			m.Pos.VelX += vmath.Mul(steerX, dt)
			m.Pos.VelY += vmath.Mul(steerY, dt)

			m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
			m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)

			AddFlareParticle(m)
		}

		px, py := vmath.ToInt(m.Pos.PreciseX), vmath.ToInt(m.Pos.PreciseY)

		// Simple hit check
		tDx := px - m.Target.X
		tDy := py - m.Target.Y
		if tDx*tDx+tDy*tDy < 4 {
			m.Active = false
		}

		// Out of bounds
		if px < 0 || px >= screenWidth || py < 0 || py >= screenHeight {
			m.Active = false
		}

		UpdateTrail(m)
	}
}

// --- Particle System ---

func UpdateTrail(m *Missile) {
	live := m.Trail[:0]
	for _, p := range m.Trail {
		p.Age++
		if p.Age < p.MaxAge {
			p.X += p.VelX
			p.Y += p.VelY
			live = append(live, p)
		}
	}
	m.Trail = live
}

func AddSmokeParticle(m *Missile) {
	m.Trail = append(m.Trail, Particle{
		X:          m.Pos.PreciseX,
		Y:          m.Pos.PreciseY,
		VelX:       0,
		VelY:       0,
		MaxAge:     20,
		Char:       '#',
		ColorStart: terminal.RGB{R: 200, G: 200, B: 200},
		ColorEnd:   terminal.RGB{R: 50, G: 50, B: 60},
	})
}

func AddHelixParticle(m *Missile, offX, offY int64, color terminal.RGB) {
	m.Trail = append(m.Trail, Particle{
		X:          m.Pos.PreciseX + offX,
		Y:          m.Pos.PreciseY + offY,
		VelX:       0,
		VelY:       0,
		MaxAge:     15,
		Char:       '·',
		ColorStart: color,
		ColorEnd:   terminal.RGB{R: 0, G: 0, B: 50},
	})
}

func AddFlareParticle(m *Missile) {
	velX, velY := vmath.Normalize2D(m.Pos.VelX, m.Pos.VelY)
	m.Trail = append(m.Trail, Particle{
		X:          m.Pos.PreciseX - vmath.Mul(velX, vmath.FromInt(1)),
		Y:          m.Pos.PreciseY - vmath.Mul(velY, vmath.FromInt(1)),
		VelX:       0,
		VelY:       0,
		MaxAge:     8,
		Char:       '▒',
		ColorStart: ColorFire,
		ColorEnd:   terminal.RGB{R: 100, G: 0, B: 0},
	})
}

// --- Rendering ---

func RenderMissiles(buf *render.RenderBuffer, missiles []*Missile) {
	for _, m := range missiles {
		// Draw Trail
		for _, p := range m.Trail {
			screenX := vmath.ToInt(p.X)
			screenY := vmath.ToInt(p.Y)

			if screenX < 0 || screenX >= screenWidth || screenY < 0 || screenY >= screenHeight {
				continue
			}

			t := int64(p.Age) * vmath.Scale / int64(p.MaxAge)
			color := render.LerpRGBFixed(p.ColorStart, p.ColorEnd, t)

			char := p.Char
			if m.Type == MissileKinetic {
				if p.Age > 5 {
					char = ':'
				}
				if p.Age > 10 {
					char = '.'
				}
			}
			buf.Set(screenX, screenY, char, color, ColorBg, render.BlendAddFg, 1.0, terminal.AttrNone)
		}

		// Draw Missile Body
		if m.Active {
			screenX := vmath.ToInt(m.Pos.PreciseX)
			screenY := vmath.ToInt(m.Pos.PreciseY)

			if screenX >= 0 && screenX < screenWidth && screenY >= 0 && screenY < screenHeight {
				var char rune
				var color terminal.RGB

				angle := math.Atan2(float64(m.Pos.VelY), float64(m.Pos.VelX))

				switch m.Type {
				case MissileKinetic:
					char = AngleToChar(angle)
					color = terminal.RGB{R: 255, G: 255, B: 255}
				case MissileHelix:
					chars := []rune{'+', 'x'}
					char = chars[(m.Age/5)%2]
					color = ColorCyan
				case MissileSeeker:
					char = AngleToArrow(angle)
					color = ColorFire
				}
				buf.Set(screenX, screenY, char, color, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
			}
		}
	}
}

func AngleToChar(rad float64) rune {
	if rad < 0 {
		rad += math.Pi
	}
	deg := rad * 180 / math.Pi
	if deg < 22.5 || deg > 157.5 {
		return '-'
	}
	if deg < 67.5 {
		return '\\'
	}
	if deg < 112.5 {
		return '|'
	}
	return '/'
}

func AngleToArrow(rad float64) rune {
	deg := rad * 180 / math.Pi
	if deg >= -22.5 && deg < 22.5 {
		return '►'
	}
	if deg >= 22.5 && deg < 67.5 {
		return '◢'
	}
	if deg >= 67.5 && deg < 112.5 {
		return '▼'
	}
	if deg >= 112.5 && deg < 157.5 {
		return '◣'
	}
	if deg >= 157.5 || deg < -157.5 {
		return '◄'
	}
	if deg >= -157.5 && deg < -112.5 {
		return '◤'
	}
	if deg >= -112.5 && deg < -67.5 {
		return '▲'
	}
	return '◥'
}

func DrawString(buf *render.RenderBuffer, x, y int, s string, color terminal.RGB) {
	for i, r := range s {
		if x+i < screenWidth {
			buf.SetFgOnly(x+i, y, r, color, terminal.AttrNone)
		}
	}
}

func MissileTypeName(t MissileType) string {
	switch t {
	case MissileKinetic:
		return "KINETIC DART"
	case MissileHelix:
		return "HELIX PHASER"
	case MissileSeeker:
		return "SEEKER SWARM"
	}
	return "?"
}