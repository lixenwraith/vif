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
	ColorBg     = terminal.RGB{R: 26, G: 27, B: 38}
	ColorSmoke  = terminal.RGB{R: 100, G: 100, B: 110}
	ColorFire   = terminal.RGB{R: 255, G: 160, B: 50}
	ColorCyan   = terminal.RGB{R: 0, G: 255, B: 255}
	ColorPink   = terminal.RGB{R: 255, G: 0, B: 255}
	ColorGold   = terminal.RGB{R: 255, G: 215, B: 0}
	ColorGreen  = terminal.RGB{R: 50, G: 255, B: 50}
	ColorPurple = terminal.RGB{R: 180, G: 100, B: 255}
	ColorWhite  = terminal.RGB{R: 255, G: 255, B: 255}
	ColorRed    = terminal.RGB{R: 255, G: 60, B: 60}
)

// --- Types ---

type MissileType int

const (
	MissileKinetic MissileType = iota
	MissileHelix
	MissileSeeker
	MissileCluster
	MissileLaser
	MissileWave
	MissileSpiral
	MissileBounce
	MissileCount // Sentinel for cycling
)

type Particle struct {
	X, Y       int64
	VelX, VelY int64
	Age        int
	MaxAge     int
	Char       rune
	ColorStart terminal.RGB
	ColorEnd   terminal.RGB
	Scale      float64 // Size multiplier for intensity
}

type Missile struct {
	Type   MissileType
	Active bool
	Pos    core.Kinetic
	Origin core.Point
	Target core.Point

	Age       int64
	Phase     int64
	SteerVecX int64
	SteerVecY int64

	// Cluster submunitions
	Children []*Missile

	// Bounce state
	Bounces int

	// Spiral state
	Angle int64

	Trail []Particle
}

var (
	screenWidth  int
	screenHeight int
	globalRng    = vmath.NewFastRand(uint64(time.Now().UnixNano()))
)

func main() {
	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		panic(err)
	}
	defer term.Fini()
	term.SetCursorVisible(false)

	screenWidth, screenHeight = term.Size()
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, screenWidth, screenHeight)

	missiles := make([]*Missile, 0)
	targets := make([]core.Point, 3)
	updateTargets(targets)

	currentTargetIdx := 1
	currentType := MissileKinetic
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

	running := true
	for running {
		select {
		case ev := <-inputCh:
			if ev.Type == terminal.EventKey {
				switch ev.Key {
				case terminal.KeyEscape, terminal.KeyCtrlC:
					running = false
				case terminal.KeySpace:
					m := SpawnMissile(currentType, origin, targets[currentTargetIdx])
					missiles = append(missiles, m)
				case terminal.KeyRune:
					if ev.Rune == ' ' {
						m := SpawnMissile(currentType, origin, targets[currentTargetIdx])
						missiles = append(missiles, m)
					}
					if ev.Rune >= '1' && ev.Rune <= '8' {
						currentType = MissileType(ev.Rune - '1')
					}
				case terminal.KeyUp:
					currentTargetIdx = (currentTargetIdx - 1 + len(targets)) % len(targets)
				case terminal.KeyDown:
					currentTargetIdx = (currentTargetIdx + 1) % len(targets)
				case terminal.KeyLeft:
					currentType = (currentType - 1 + MissileCount) % MissileCount
				case terminal.KeyRight:
					currentType = (currentType + 1) % MissileCount
				}
			}

		case resize := <-resizeCh:
			screenWidth, screenHeight = resize.Width, resize.Height
			buf.Resize(screenWidth, screenHeight)
			updateTargets(targets)
			origin = core.Point{X: 10, Y: screenHeight / 2}
			term.Sync()

		case <-ticker.C:
			UpdateMissiles(missiles)

			active := missiles[:0]
			for _, m := range missiles {
				if m.Active || len(m.Trail) > 0 || hasActiveChildren(m) {
					active = append(active, m)
				}
			}
			missiles = active

			buf.Clear()

			// Draw targets
			for i, t := range targets {
				char, color := 'o', terminal.RGB{R: 80, G: 80, B: 80}
				if i == currentTargetIdx {
					char, color = '◎', ColorRed
				}
				if t.X < screenWidth && t.Y < screenHeight {
					buf.Set(t.X, t.Y, char, color, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
				}
			}

			// Draw origin
			buf.Set(origin.X, origin.Y, '▶', ColorGreen, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)

			// Draw UI
			uiText := fmt.Sprintf("[%s] ←/→:Type ↑/↓:Target Space:Fire Esc:Quit",
				MissileTypeName(currentType))
			DrawString(buf, 2, screenHeight-1, uiText, terminal.RGB{R: 180, G: 180, B: 180})

			// Draw type legend
			for i := 0; i < int(MissileCount); i++ {
				color := terminal.RGB{R: 100, G: 100, B: 100}
				if MissileType(i) == currentType {
					color = ColorGold
				}
				DrawString(buf, 2, 1+i, fmt.Sprintf("%d:%s", i+1, MissileTypeName(MissileType(i))), color)
			}

			RenderMissiles(buf, missiles)
			buf.FlushToTerminal(term)
		}
	}
}

func hasActiveChildren(m *Missile) bool {
	for _, c := range m.Children {
		if c.Active || len(c.Trail) > 0 {
			return true
		}
	}
	return false
}

func updateTargets(targets []core.Point) {
	targets[0] = core.Point{X: screenWidth - 10, Y: 5}
	targets[1] = core.Point{X: screenWidth - 10, Y: screenHeight / 2}
	targets[2] = core.Point{X: screenWidth - 10, Y: screenHeight - 5}
}

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
		Trail: make([]Particle, 0, 100),
	}

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
		speed := vmath.FromInt(55)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed) - vmath.FromInt(15)

	case MissileHelix:
		speed := vmath.FromInt(35)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed)

	case MissileSeeker:
		speed := vmath.FromInt(12)
		perpX, perpY := vmath.Perpendicular(dirX, dirY)
		if origin.Y > target.Y {
			perpX, perpY = -perpX, -perpY
		}
		m.Pos.VelX = vmath.Mul(perpX, speed)
		m.Pos.VelY = vmath.Mul(perpY, speed)

	case MissileCluster:
		speed := vmath.FromInt(40)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed) - vmath.FromInt(8)

	case MissileLaser:
		// Instant - no velocity, handled in update

	case MissileWave:
		speed := vmath.FromInt(45)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed)

	case MissileSpiral:
		m.Angle = 0

	case MissileBounce:
		speed := vmath.FromInt(50)
		m.Pos.VelX = vmath.Mul(dirX, speed)
		m.Pos.VelY = vmath.Mul(dirY, speed)
		m.Bounces = 3
	}

	return m
}

func UpdateMissiles(missiles []*Missile) {
	dt := vmath.FromFloat(1.0 / 60.0)

	for _, m := range missiles {
		if !m.Active {
			UpdateTrail(m)
			for _, c := range m.Children {
				if c.Active {
					updateSingleMissile(c, dt)
				}
				UpdateTrail(c)
			}
			continue
		}

		updateSingleMissile(m, dt)
		UpdateTrail(m)
	}
}

func updateSingleMissile(m *Missile, dt int64) {
	if !m.Active {
		return
	}
	m.Age++

	switch m.Type {
	case MissileKinetic:
		gravity := vmath.FromInt(25)
		m.Pos.VelY += vmath.Mul(gravity, dt)
		m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
		m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)

		// Dense smoke trail
		if m.Age%2 == 0 {
			speed := vmath.Magnitude(m.Pos.VelX, m.Pos.VelY)
			intensity := float64(speed) / float64(vmath.FromInt(80))
			if intensity > 1 {
				intensity = 1
			}
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
				VelX: -m.Pos.VelX / 20, VelY: -m.Pos.VelY / 20,
				MaxAge: 25, Char: '░',
				ColorStart: terminal.RGB{R: 255, G: 200, B: 150},
				ColorEnd:   terminal.RGB{R: 60, G: 60, B: 70},
				Scale:      intensity,
			})
		}
		// Sparks
		if m.Age%4 == 0 {
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
				VelX:   int64(globalRng.Intn(int(vmath.Scale*2))) - vmath.Scale,
				VelY:   int64(globalRng.Intn(int(vmath.Scale*2))) - vmath.Scale,
				MaxAge: 8, Char: '·',
				ColorStart: ColorFire, ColorEnd: ColorRed,
			})
		}

	case MissileHelix:
		m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
		m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)
		m.Phase += vmath.FromInt(12)

		baseX, baseY := vmath.Normalize2D(m.Pos.VelX, m.Pos.VelY)
		perpX, perpY := vmath.Perpendicular(baseX, baseY)

		// Triple helix with phase offsets
		for i := 0; i < 3; i++ {
			phase := m.Phase + vmath.FromInt(i*120)
			amp := vmath.FromFloat(2.5)
			sinVal := vmath.Sin(phase)
			cosVal := vmath.Cos(phase)

			offX := vmath.Mul(vmath.Mul(perpX, amp), sinVal)
			offY := vmath.Mul(vmath.Mul(perpY, amp), sinVal)

			colors := []terminal.RGB{ColorCyan, ColorPink, ColorPurple}
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX + offX, Y: m.Pos.PreciseY + offY,
				MaxAge: 18, Char: '∘',
				ColorStart: colors[i], ColorEnd: terminal.RGB{R: 20, G: 20, B: 40},
				Scale: 0.5 + 0.5*float64(cosVal)/float64(vmath.Scale),
			})
		}

	case MissileSeeker:
		targetX := vmath.FromInt(m.Target.X)
		targetY := vmath.FromInt(m.Target.Y)
		dx := targetX - m.Pos.PreciseX
		dy := targetY - m.Pos.PreciseY
		dist := vmath.Magnitude(dx, dy)

		if dist < vmath.FromInt(2) {
			m.Active = false
			spawnExplosion(m)
			return
		}

		maxSpeed := vmath.FromInt(50)
		steerForce := vmath.FromInt(100)

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

		// Engine flare
		velX, velY := vmath.Normalize2D(m.Pos.VelX, m.Pos.VelY)
		m.Trail = append(m.Trail, Particle{
			X:      m.Pos.PreciseX - vmath.Mul(velX, vmath.Scale),
			Y:      m.Pos.PreciseY - vmath.Mul(velY, vmath.Scale),
			MaxAge: 10, Char: '▓',
			ColorStart: ColorWhite, ColorEnd: ColorFire,
		})
		// Side exhaust
		perpX, perpY := vmath.Perpendicular(velX, velY)
		for _, sign := range []int64{1, -1} {
			m.Trail = append(m.Trail, Particle{
				X:      m.Pos.PreciseX - vmath.Mul(velX, vmath.Scale/2) + sign*vmath.Mul(perpX, vmath.Scale/3),
				Y:      m.Pos.PreciseY - vmath.Mul(velY, vmath.Scale/2) + sign*vmath.Mul(perpY, vmath.Scale/3),
				MaxAge: 6, Char: '·',
				ColorStart: ColorCyan, ColorEnd: ColorBg,
			})
		}

	case MissileCluster:
		gravity := vmath.FromInt(18)
		m.Pos.VelY += vmath.Mul(gravity, dt)
		m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
		m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)

		if m.Age%3 == 0 {
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
				MaxAge: 15, Char: '░',
				ColorStart: ColorGold, ColorEnd: ColorSmoke,
			})
		}

		// Split at apex or after time
		if m.Pos.VelY > 0 && m.Age > 20 && len(m.Children) == 0 {
			m.Active = false
			for i := 0; i < 5; i++ {
				angle := float64(i)*math.Pi/2.5 - math.Pi/2
				child := &Missile{
					Type:   MissileSeeker,
					Active: true,
					Origin: core.Point{X: vmath.ToInt(m.Pos.PreciseX), Y: vmath.ToInt(m.Pos.PreciseY)},
					Target: m.Target,
					Pos: core.Kinetic{
						PreciseX: m.Pos.PreciseX,
						PreciseY: m.Pos.PreciseY,
						VelX:     vmath.FromFloat(math.Cos(angle) * 20),
						VelY:     vmath.FromFloat(math.Sin(angle) * 20),
					},
					Trail: make([]Particle, 0, 50),
				}
				m.Children = append(m.Children, child)
			}
			// Burst effect
			for i := 0; i < 12; i++ {
				angle := float64(i) * math.Pi / 6
				m.Trail = append(m.Trail, Particle{
					X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
					VelX:   vmath.FromFloat(math.Cos(angle) * 3),
					VelY:   vmath.FromFloat(math.Sin(angle) * 3),
					MaxAge: 12, Char: '*',
					ColorStart: ColorWhite, ColorEnd: ColorGold,
				})
			}
		}

	case MissileLaser:
		if m.Age == 1 {
			// Draw instant beam
			x1, y1 := m.Origin.X, m.Origin.Y
			x2, y2 := m.Target.X, m.Target.Y
			steps := max(vmath.IntAbs(x2-x1), vmath.IntAbs(y2-y1))
			for i := 0; i <= steps; i++ {
				t := float64(i) / float64(steps)
				px := vmath.FromFloat(float64(x1) + t*float64(x2-x1))
				py := vmath.FromFloat(float64(y1) + t*float64(y2-y1))
				m.Trail = append(m.Trail, Particle{
					X: px, Y: py,
					MaxAge: 15 - i/4, Char: '═',
					ColorStart: ColorWhite, ColorEnd: ColorCyan,
					Scale: 1.0 - t*0.5,
				})
			}
			// Impact flash
			for i := 0; i < 8; i++ {
				angle := float64(i) * math.Pi / 4
				m.Trail = append(m.Trail, Particle{
					X: vmath.FromInt(x2), Y: vmath.FromInt(y2),
					VelX:   vmath.FromFloat(math.Cos(angle) * 4),
					VelY:   vmath.FromFloat(math.Sin(angle) * 4),
					MaxAge: 10, Char: '✦',
					ColorStart: ColorWhite, ColorEnd: ColorCyan,
				})
			}
		}
		if m.Age > 3 {
			m.Active = false
		}

	case MissileWave:
		m.Phase += vmath.FromInt(8)
		baseVelX, baseVelY := vmath.Normalize2D(m.Pos.VelX, m.Pos.VelY)
		perpX, perpY := vmath.Perpendicular(baseVelX, baseVelY)

		// Sinusoidal offset
		amp := vmath.FromFloat(4.0)
		sinVal := vmath.Sin(m.Phase)
		offsetX := vmath.Mul(vmath.Mul(perpX, amp), sinVal)
		offsetY := vmath.Mul(vmath.Mul(perpY, amp), sinVal)

		m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt) + vmath.Mul(offsetX, dt*3)
		m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt) + vmath.Mul(offsetY, dt*3)

		// Rainbow trail
		hue := int(m.Age) % 256
		color := hueToRGB(hue)
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			MaxAge: 20, Char: '~',
			ColorStart: color, ColorEnd: ColorBg,
		})

	case MissileSpiral:
		m.Angle += vmath.FromFloat(0.15)
		radius := vmath.FromFloat(float64(m.Age) * 0.3)
		if radius > vmath.FromInt(25) {
			m.Active = false
			return
		}

		centerX := vmath.FromInt(m.Origin.X)
		centerY := vmath.FromInt(m.Origin.Y)
		cos := vmath.Cos(m.Angle)
		sin := vmath.Sin(m.Angle)

		m.Pos.PreciseX = centerX + vmath.Mul(cos, radius)
		m.Pos.PreciseY = centerY + vmath.Mul(sin, radius)/2 // Aspect correction

		// Dual spiral trail
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			MaxAge: 30, Char: '◦',
			ColorStart: ColorGreen, ColorEnd: ColorBg,
		})
		// Opposite arm
		m.Trail = append(m.Trail, Particle{
			X:      centerX - vmath.Mul(cos, radius),
			Y:      centerY - vmath.Mul(sin, radius)/2,
			MaxAge: 30, Char: '◦',
			ColorStart: ColorPurple, ColorEnd: ColorBg,
		})

	case MissileBounce:
		m.Pos.PreciseX += vmath.Mul(m.Pos.VelX, dt)
		m.Pos.PreciseY += vmath.Mul(m.Pos.VelY, dt)

		px, py := vmath.ToInt(m.Pos.PreciseX), vmath.ToInt(m.Pos.PreciseY)
		bounced := false

		if px <= 0 || px >= screenWidth-1 {
			m.Pos.VelX = -m.Pos.VelX
			bounced = true
		}
		if py <= 0 || py >= screenHeight-2 {
			m.Pos.VelY = -m.Pos.VelY
			bounced = true
		}

		if bounced {
			m.Bounces--
			// Bounce spark
			for i := 0; i < 6; i++ {
				angle := float64(globalRng.Intn(628)) / 100
				m.Trail = append(m.Trail, Particle{
					X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
					VelX:   vmath.FromFloat(math.Cos(angle) * 5),
					VelY:   vmath.FromFloat(math.Sin(angle) * 5),
					MaxAge: 8, Char: '✧',
					ColorStart: ColorWhite, ColorEnd: ColorGold,
				})
			}
		}

		if m.Bounces < 0 {
			m.Active = false
			spawnExplosion(m)
			return
		}

		// Comet trail
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			MaxAge: 12, Char: '▪',
			ColorStart: ColorGold, ColorEnd: ColorRed,
		})
	}

	// Bounds and hit check
	px, py := vmath.ToInt(m.Pos.PreciseX), vmath.ToInt(m.Pos.PreciseY)
	if m.Type != MissileLaser && m.Type != MissileSpiral {
		tDx := px - m.Target.X
		tDy := py - m.Target.Y
		if tDx*tDx+tDy*tDy < 4 {
			m.Active = false
			spawnExplosion(m)
		}
		if px < 0 || px >= screenWidth || py < 0 || py >= screenHeight {
			m.Active = false
		}
	}
}

func spawnExplosion(m *Missile) {
	for i := 0; i < 16; i++ {
		angle := float64(i) * math.Pi / 8
		speed := 2.0 + float64(globalRng.Intn(30))/10
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			VelX:   vmath.FromFloat(math.Cos(angle) * speed),
			VelY:   vmath.FromFloat(math.Sin(angle) * speed),
			MaxAge: 15, Char: '✦',
			ColorStart: ColorWhite, ColorEnd: ColorFire,
		})
	}
}

func UpdateTrail(m *Missile) {
	live := m.Trail[:0]
	for i := range m.Trail {
		p := &m.Trail[i]
		p.Age++
		if p.Age < p.MaxAge {
			p.X += p.VelX
			p.Y += p.VelY
			live = append(live, *p)
		}
	}
	m.Trail = live
}

func RenderMissiles(buf *render.RenderBuffer, missiles []*Missile) {
	for _, m := range missiles {
		renderMissileTrail(buf, m)
		renderMissileBody(buf, m)

		for _, c := range m.Children {
			renderMissileTrail(buf, c)
			renderMissileBody(buf, c)
		}
	}
}

func renderMissileTrail(buf *render.RenderBuffer, m *Missile) {
	for _, p := range m.Trail {
		screenX := vmath.ToInt(p.X)
		screenY := vmath.ToInt(p.Y)

		if screenX < 0 || screenX >= screenWidth || screenY < 0 || screenY >= screenHeight-1 {
			continue
		}

		t := int64(p.Age) * vmath.Scale / int64(p.MaxAge)
		color := render.LerpRGBFixed(p.ColorStart, p.ColorEnd, t)
		alpha := 1.0 - float64(p.Age)/float64(p.MaxAge)
		if p.Scale > 0 {
			alpha *= p.Scale
		}

		char := p.Char
		if m.Type == MissileKinetic {
			switch {
			case p.Age > 15:
				char = '.'
			case p.Age > 8:
				char = '·'
			}
		}
		buf.Set(screenX, screenY, char, color, ColorBg, render.BlendAddFg, alpha, terminal.AttrNone)
	}
}

func renderMissileBody(buf *render.RenderBuffer, m *Missile) {
	if !m.Active {
		return
	}

	screenX := vmath.ToInt(m.Pos.PreciseX)
	screenY := vmath.ToInt(m.Pos.PreciseY)

	if screenX < 0 || screenX >= screenWidth || screenY < 0 || screenY >= screenHeight-1 {
		return
	}

	var char rune
	var color terminal.RGB
	angle := math.Atan2(float64(m.Pos.VelY), float64(m.Pos.VelX))

	switch m.Type {
	case MissileKinetic:
		char = AngleToChar(angle)
		color = ColorWhite
	case MissileHelix:
		chars := []rune{'✧', '✦', '★'}
		char = chars[(m.Age/4)%3]
		color = render.LerpRGBFixed(ColorCyan, ColorPink, vmath.Sin(m.Phase))
	case MissileSeeker:
		char = AngleToArrow(angle)
		color = ColorFire
	case MissileCluster:
		char = '◆'
		color = ColorGold
	case MissileLaser:
		char = '⚡'
		color = ColorCyan
	case MissileWave:
		char = '≋'
		color = hueToRGB(int(m.Age) % 256)
	case MissileSpiral:
		char = '✺'
		color = ColorGreen
	case MissileBounce:
		char = '●'
		color = ColorGold
	}

	buf.Set(screenX, screenY, char, color, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
}

func hueToRGB(hue int) terminal.RGB {
	h := float64(hue) / 256.0 * 6.0
	x := 1.0 - math.Abs(math.Mod(h, 2)-1)
	var r, g, b float64
	switch int(h) {
	case 0:
		r, g, b = 1, x, 0
	case 1:
		r, g, b = x, 1, 0
	case 2:
		r, g, b = 0, 1, x
	case 3:
		r, g, b = 0, x, 1
	case 4:
		r, g, b = x, 0, 1
	default:
		r, g, b = 1, 0, x
	}
	return terminal.RGB{R: uint8(r * 255), G: uint8(g * 255), B: uint8(b * 255)}
}

func AngleToChar(rad float64) rune {
	if rad < 0 {
		rad += math.Pi * 2
	}
	deg := rad * 180 / math.Pi
	switch {
	case deg < 22.5 || deg >= 337.5:
		return '→'
	case deg < 67.5:
		return '↘'
	case deg < 112.5:
		return '↓'
	case deg < 157.5:
		return '↙'
	case deg < 202.5:
		return '←'
	case deg < 247.5:
		return '↖'
	case deg < 292.5:
		return '↑'
	default:
		return '↗'
	}
}

func AngleToArrow(rad float64) rune {
	deg := rad * 180 / math.Pi
	switch {
	case deg >= -22.5 && deg < 22.5:
		return '▸'
	case deg >= 22.5 && deg < 67.5:
		return '◢'
	case deg >= 67.5 && deg < 112.5:
		return '▾'
	case deg >= 112.5 && deg < 157.5:
		return '◣'
	case deg >= 157.5 || deg < -157.5:
		return '◂'
	case deg >= -157.5 && deg < -112.5:
		return '◤'
	case deg >= -112.5 && deg < -67.5:
		return '▴'
	default:
		return '◥'
	}
}

func DrawString(buf *render.RenderBuffer, x, y int, s string, color terminal.RGB) {
	for i, r := range s {
		if x+i < screenWidth {
			buf.SetFgOnly(x+i, y, r, color, terminal.AttrNone)
		}
	}
}

func MissileTypeName(t MissileType) string {
	names := []string{
		"KINETIC DART",
		"HELIX PHASER",
		"SEEKER SWARM",
		"CLUSTER BOMB",
		"LASER BEAM",
		"WAVE RIDER",
		"SPIRAL NOVA",
		"BOUNCE BALL",
	}
	if int(t) < len(names) {
		return names[t]
	}
	return "?"
}