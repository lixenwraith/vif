package main

import (
	"fmt"
	"math"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// --- Visual Constants ---
var (
	ColorBg     = color.RGB{R: 26, G: 27, B: 38}
	ColorSmoke  = color.RGB{R: 100, G: 100, B: 110}
	ColorFire   = color.RGB{R: 255, G: 160, B: 50}
	ColorCyan   = color.RGB{R: 0, G: 255, B: 255}
	ColorPink   = color.RGB{R: 255, G: 0, B: 255}
	ColorGold   = color.RGB{R: 255, G: 215, B: 0}
	ColorGreen  = color.RGB{R: 50, G: 255, B: 50}
	ColorPurple = color.RGB{R: 180, G: 100, B: 255}
	ColorWhite  = color.RGB{R: 255, G: 255, B: 255}
	ColorRed    = color.RGB{R: 255, G: 60, B: 60}
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
	X, Y       float64
	VelX, VelY float64
	Age        int
	MaxAge     int
	Char       rune
	ColorStart color.RGB
	ColorEnd   color.RGB
	Scale      float64 // Size multiplier for intensity
}

type Missile struct {
	Type   MissileType
	Active bool
	Pos    physics.Kinetic
	Origin vmath.Point
	Target vmath.Point

	Age   int
	Phase float64

	// Cluster submunitions
	Children []*Missile

	// Bounce state
	Bounces int

	// Spiral state
	Angle float64

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
	targets := make([]vmath.Point, 3)
	updateTargets(targets)

	currentTargetIdx := 1
	currentType := MissileKinetic
	origin := vmath.Point{X: 10, Y: screenHeight / 2}

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
			origin = vmath.Point{X: 10, Y: screenHeight / 2}
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
				char, c := 'o', color.RGB{R: 80, G: 80, B: 80}
				if i == currentTargetIdx {
					char, c = '◎', ColorRed
				}
				if t.X < screenWidth && t.Y < screenHeight {
					buf.Set(t.X, t.Y, char, c, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
				}
			}

			// Draw origin
			buf.Set(origin.X, origin.Y, '▶', ColorGreen, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)

			// Draw UI
			uiText := fmt.Sprintf("[%s] ←/→:Type ↑/↓:Target Space:Fire Esc:Quit",
				MissileTypeName(currentType))
			DrawString(buf, 2, screenHeight-1, uiText, color.RGB{R: 180, G: 180, B: 180})

			// Draw type legend
			for i := range int(MissileCount) {
				c := color.RGB{R: 100, G: 100, B: 100}
				if MissileType(i) == currentType {
					c = ColorGold
				}
				DrawString(buf, 2, 1+i, fmt.Sprintf("%d:%s", i+1, MissileTypeName(MissileType(i))), c)
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

func updateTargets(targets []vmath.Point) {
	targets[0] = vmath.Point{X: screenWidth - 10, Y: 5}
	targets[1] = vmath.Point{X: screenWidth - 10, Y: screenHeight / 2}
	targets[2] = vmath.Point{X: screenWidth - 10, Y: screenHeight - 5}
}

func SpawnMissile(t MissileType, origin, target vmath.Point) *Missile {
	originX, originY := origin.CenterF()
	targetX, targetY := target.CenterF()
	m := &Missile{
		Type:   t,
		Active: true,
		Origin: origin,
		Target: target,
		Pos: physics.Kinetic{
			PreciseX: originX,
			PreciseY: originY,
		},
		Trail: make([]Particle, 0, 100),
	}

	dirX, dirY := vmath.Normalize2DF(targetX-originX, targetY-originY)

	switch t {
	case MissileKinetic:
		const speed = 55.0
		m.Pos.VelX = dirX * speed
		m.Pos.VelY = dirY*speed - 15.0

	case MissileHelix:
		const speed = 35.0
		m.Pos.VelX = dirX * speed
		m.Pos.VelY = dirY * speed

	case MissileSeeker:
		const speed = 12.0
		perpX, perpY := vmath.PerpendicularF(dirX, dirY)
		if origin.Y > target.Y {
			perpX, perpY = -perpX, -perpY
		}
		m.Pos.VelX = perpX * speed
		m.Pos.VelY = perpY * speed

	case MissileCluster:
		const speed = 40.0
		m.Pos.VelX = dirX * speed
		m.Pos.VelY = dirY*speed - 8.0

	case MissileLaser:
		// Instant - no velocity, handled in update

	case MissileWave:
		const speed = 45.0
		m.Pos.VelX = dirX * speed
		m.Pos.VelY = dirY * speed

	case MissileSpiral:
		m.Angle = 0

	case MissileBounce:
		const speed = 50.0
		m.Pos.VelX = dirX * speed
		m.Pos.VelY = dirY * speed
		m.Bounces = 3
	}

	return m
}

func UpdateMissiles(missiles []*Missile) {
	const dt = 1.0 / 60.0

	for _, m := range missiles {
		if !m.Active {
			UpdateTrail(m, dt)
			for _, c := range m.Children {
				if c.Active {
					updateSingleMissile(c, dt)
				}
				UpdateTrail(c, dt)
			}
			continue
		}

		updateSingleMissile(m, dt)
		UpdateTrail(m, dt)
	}
}

func updateSingleMissile(m *Missile, dt float64) {
	if !m.Active {
		return
	}
	m.Age++

	switch m.Type {
	case MissileKinetic:
		m.Pos.VelY += 25.0 * dt
		physics.IntegratePosition(&m.Pos, dt)

		// Dense smoke trail
		if m.Age%2 == 0 {
			speed := vmath.MagnitudeF(m.Pos.VelX, m.Pos.VelY)
			intensity := speed / 80.0
			if intensity > 1 {
				intensity = 1
			}
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
				VelX: -m.Pos.VelX / 20, VelY: -m.Pos.VelY / 20,
				MaxAge: 25, Char: '░',
				ColorStart: color.RGB{R: 255, G: 200, B: 150},
				ColorEnd:   color.RGB{R: 60, G: 60, B: 70},
				Scale:      intensity,
			})
		}
		// Sparks
		if m.Age%4 == 0 {
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
				VelX:   globalRng.Float64()*2.0 - 1.0,
				VelY:   globalRng.Float64()*2.0 - 1.0,
				MaxAge: 8, Char: '·',
				ColorStart: ColorFire, ColorEnd: ColorRed,
			})
		}

	case MissileHelix:
		physics.IntegratePosition(&m.Pos, dt)
		m.Phase += vmath.DegToRad(12.0)

		baseX, baseY := vmath.Normalize2DF(m.Pos.VelX, m.Pos.VelY)
		perpX, perpY := vmath.PerpendicularF(baseX, baseY)

		// Triple helix with phase offsets
		for i := range 3 {
			phase := m.Phase + vmath.DegToRad(float64(i)*120.0)
			sinVal := vmath.SinF(phase)
			cosVal := vmath.CosF(phase)

			offX := perpX * 2.5 * sinVal
			offY := perpY * 2.5 * sinVal

			colors := []color.RGB{ColorCyan, ColorPink, ColorPurple}
			m.Trail = append(m.Trail, Particle{
				X: m.Pos.PreciseX + offX, Y: m.Pos.PreciseY + offY,
				MaxAge: 18, Char: '∘',
				ColorStart: colors[i], ColorEnd: color.RGB{R: 20, G: 20, B: 40},
				Scale: 0.5 + 0.5*cosVal,
			})
		}

	case MissileSeeker:
		targetX, targetY := m.Target.CenterF()
		dx := targetX - m.Pos.PreciseX
		dy := targetY - m.Pos.PreciseY
		dist := vmath.MagnitudeF(dx, dy)

		if dist < 2.0 {
			m.Active = false
			spawnExplosion(m)
			return
		}

		const (
			maxSpeed   = 50.0
			steerForce = 100.0
		)

		desiredX, desiredY := vmath.Normalize2DF(dx, dy)
		desiredX *= maxSpeed
		desiredY *= maxSpeed

		steerX := desiredX - m.Pos.VelX
		steerY := desiredY - m.Pos.VelY
		steerX, steerY = vmath.ClampMagnitudeF(steerX, steerY, steerForce)

		m.Pos.VelX += steerX * dt
		m.Pos.VelY += steerY * dt
		m.Pos.VelX, m.Pos.VelY = physics.CapSpeed(m.Pos.VelX, m.Pos.VelY, maxSpeed)
		physics.IntegratePosition(&m.Pos, dt)

		// Engine flare
		velX, velY := vmath.Normalize2DF(m.Pos.VelX, m.Pos.VelY)
		m.Trail = append(m.Trail, Particle{
			X:      m.Pos.PreciseX - velX,
			Y:      m.Pos.PreciseY - velY,
			MaxAge: 10, Char: '▓',
			ColorStart: ColorWhite, ColorEnd: ColorFire,
		})
		// Side exhaust
		perpX, perpY := vmath.PerpendicularF(velX, velY)
		for _, sign := range []float64{1, -1} {
			m.Trail = append(m.Trail, Particle{
				X:      m.Pos.PreciseX - velX/2.0 + sign*perpX/3.0,
				Y:      m.Pos.PreciseY - velY/2.0 + sign*perpY/3.0,
				MaxAge: 6, Char: '·',
				ColorStart: ColorCyan, ColorEnd: ColorBg,
			})
		}

	case MissileCluster:
		m.Pos.VelY += 18.0 * dt
		physics.IntegratePosition(&m.Pos, dt)

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
			for i := range 5 {
				angle := float64(i)*math.Pi/2.5 - math.Pi/2
				child := &Missile{
					Type:   MissileSeeker,
					Active: true,
					Origin: vmath.PointAtF(m.Pos.PreciseX, m.Pos.PreciseY),
					Target: m.Target,
					Pos: physics.Kinetic{
						PreciseX: m.Pos.PreciseX,
						PreciseY: m.Pos.PreciseY,
						VelX:     math.Cos(angle) * 20.0,
						VelY:     math.Sin(angle) * 20.0,
					},
					Trail: make([]Particle, 0, 50),
				}
				m.Children = append(m.Children, child)
			}
			// Burst effect
			for i := range 12 {
				angle := float64(i) * math.Pi / 6
				m.Trail = append(m.Trail, Particle{
					X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
					VelX:   math.Cos(angle) * 3.0,
					VelY:   math.Sin(angle) * 3.0,
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
				px := float64(x1) + t*float64(x2-x1)
				py := float64(y1) + t*float64(y2-y1)
				m.Trail = append(m.Trail, Particle{
					X: px, Y: py,
					MaxAge: 15 - i/4, Char: '═',
					ColorStart: ColorWhite, ColorEnd: ColorCyan,
					Scale: 1.0 - t*0.5,
				})
			}
			// Impact flash
			for i := range 8 {
				angle := float64(i) * math.Pi / 4
				impactX, impactY := m.Target.CenterF()
				m.Trail = append(m.Trail, Particle{
					X: impactX, Y: impactY,
					VelX:   math.Cos(angle) * 4.0,
					VelY:   math.Sin(angle) * 4.0,
					MaxAge: 10, Char: '✦',
					ColorStart: ColorWhite, ColorEnd: ColorCyan,
				})
			}
		}
		if m.Age > 3 {
			m.Active = false
		}

	case MissileWave:
		m.Phase += vmath.DegToRad(8.0)
		baseVelX, baseVelY := vmath.Normalize2DF(m.Pos.VelX, m.Pos.VelY)
		perpX, perpY := vmath.PerpendicularF(baseVelX, baseVelY)

		// Sinusoidal offset
		sinVal := vmath.SinF(m.Phase)
		offsetX := perpX * 4.0 * sinVal
		offsetY := perpY * 4.0 * sinVal

		physics.IntegratePosition(&m.Pos, dt)
		m.Pos.PreciseX += offsetX * dt * 3.0
		m.Pos.PreciseY += offsetY * dt * 3.0

		// Rainbow trail
		hue := int(m.Age) % 256
		c := hueToRGB(hue)
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			MaxAge: 20, Char: '~',
			ColorStart: c, ColorEnd: ColorBg,
		})

	case MissileSpiral:
		m.Angle += 0.15
		radius := float64(m.Age) * 0.3
		if radius > 25.0 {
			m.Active = false
			return
		}

		centerX, centerY := m.Origin.CenterF()
		cos := vmath.CosF(m.Angle)
		sin := vmath.SinF(m.Angle)

		m.Pos.PreciseX = centerX + cos*radius
		m.Pos.PreciseY = centerY + sin*radius/2.0 // Aspect correction

		// Dual spiral trail
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			MaxAge: 30, Char: '◦',
			ColorStart: ColorGreen, ColorEnd: ColorBg,
		})
		// Opposite arm
		m.Trail = append(m.Trail, Particle{
			X:      centerX - cos*radius,
			Y:      centerY - sin*radius/2.0,
			MaxAge: 30, Char: '◦',
			ColorStart: ColorPurple, ColorEnd: ColorBg,
		})

	case MissileBounce:
		physics.IntegratePosition(&m.Pos, dt)

		bounced := physics.ReflectBoundsX(&m.Pos, 0, screenWidth)
		if physics.ReflectBoundsY(&m.Pos, 0, screenHeight-1) {
			bounced = true
		}

		if bounced {
			m.Bounces--
			// Bounce spark
			for range 6 {
				angle := float64(globalRng.Intn(628)) / 100
				m.Trail = append(m.Trail, Particle{
					X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
					VelX:   math.Cos(angle) * 5.0,
					VelY:   math.Sin(angle) * 5.0,
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
	px, py := physics.GridPos(&m.Pos)
	if m.Type != MissileLaser && m.Type != MissileSpiral {
		targetX, targetY := m.Target.CenterF()
		tDx := targetX - m.Pos.PreciseX
		tDy := targetY - m.Pos.PreciseY
		if vmath.MagnitudeSqF(tDx, tDy) < 4.0 {
			m.Active = false
			spawnExplosion(m)
		}
		if px < 0 || px >= screenWidth || py < 0 || py >= screenHeight {
			m.Active = false
		}
	}
}

func spawnExplosion(m *Missile) {
	for i := range 16 {
		angle := float64(i) * math.Pi / 8
		speed := 2.0 + float64(globalRng.Intn(30))/10
		m.Trail = append(m.Trail, Particle{
			X: m.Pos.PreciseX, Y: m.Pos.PreciseY,
			VelX:   math.Cos(angle) * speed,
			VelY:   math.Sin(angle) * speed,
			MaxAge: 15, Char: '✦',
			ColorStart: ColorWhite, ColorEnd: ColorFire,
		})
	}
}

func UpdateTrail(m *Missile, dt float64) {
	live := m.Trail[:0]
	for i := range m.Trail {
		p := &m.Trail[i]
		p.Age++
		if p.Age < p.MaxAge {
			p.X += p.VelX * dt
			p.Y += p.VelY * dt
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
		point := vmath.PointAtF(p.X, p.Y)
		screenX, screenY := point.X, point.Y

		if screenX < 0 || screenX >= screenWidth || screenY < 0 || screenY >= screenHeight-1 {
			continue
		}

		t := float64(p.Age) / float64(p.MaxAge)
		c := render.LerpRGB(p.ColorStart, p.ColorEnd, t)
		alpha := 1.0 - t
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
		buf.Set(screenX, screenY, char, c, ColorBg, render.BlendAddFg, alpha, terminal.AttrNone)
	}
}

func renderMissileBody(buf *render.RenderBuffer, m *Missile) {
	if !m.Active {
		return
	}

	screenX, screenY := physics.GridPos(&m.Pos)

	if screenX < 0 || screenX >= screenWidth || screenY < 0 || screenY >= screenHeight-1 {
		return
	}

	var char rune
	var c color.RGB
	angle := math.Atan2(m.Pos.VelY, m.Pos.VelX)

	switch m.Type {
	case MissileKinetic:
		char = AngleToChar(angle)
		c = ColorWhite
	case MissileHelix:
		chars := []rune{'✧', '✦', '★'}
		char = chars[(m.Age/4)%3]
		c = render.LerpRGB(ColorCyan, ColorPink, (vmath.SinF(m.Phase)+1.0)/2.0)
	case MissileSeeker:
		char = AngleToArrow(angle)
		c = ColorFire
	case MissileCluster:
		char = '◆'
		c = ColorGold
	case MissileLaser:
		char = '⚡'
		c = ColorCyan
	case MissileWave:
		char = '≋'
		c = hueToRGB(int(m.Age) % 256)
	case MissileSpiral:
		char = '✺'
		c = ColorGreen
	case MissileBounce:
		char = '●'
		c = ColorGold
	}

	buf.Set(screenX, screenY, char, c, ColorBg, render.BlendReplace, 1.0, terminal.AttrBold)
}

func hueToRGB(hue int) color.RGB {
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
	return color.RGB{R: uint8(r * 255), G: uint8(g * 255), B: uint8(b * 255)}
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

func DrawString(buf *render.RenderBuffer, x, y int, s string, c color.RGB) {
	for i, r := range s {
		if x+i < screenWidth {
			buf.SetFgOnly(x+i, y, r, c, terminal.AttrNone)
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
