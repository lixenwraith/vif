package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

// Part represents one composite sphere entity
type Part struct {
	Pos, Vel vmath.Vec3F
	Mass     float64
	Radius   float64
	Color    color.RGB
	Frozen   bool
	Flash    float64
}

type projected struct {
	cx, cy, radius, depth float64
	index                 int
}

const (
	targetFPS    = 30
	framePeriod  = time.Second / targetFPS
	flashSeconds = 0.2
	hudRows      = 2

	boundsX    = 16.0
	boundsY    = 8.0
	boundsZMin = 3.0
	boundsZMax = 32.0

	focalLen            = 14.0
	restitution         = 0.8
	partRadius          = 2.8
	massDefault         = 5.0
	massStep            = 0.5
	massMin             = 0.1
	massMax             = 20.0
	frozenCollisionMass = math.MaxFloat64
)

var (
	// Precomputed lighting (float64 for per-pixel shading path)
	lightX, lightY, lightZ float64
	halfX, halfY, halfZ    float64
)

func initLighting() {
	lx, ly, lz := -0.35, -0.55, 0.75
	m := math.Sqrt(lx*lx + ly*ly + lz*lz)
	lightX, lightY, lightZ = lx/m, ly/m, lz/m

	// Blinn-Phong half vector: normalize(light + view), view = (0,0,1)
	hx, hy, hz := lightX, lightY, lightZ+1.0
	m = math.Sqrt(hx*hx + hy*hy + hz*hz)
	halfX, halfY, halfZ = hx/m, hy/m, hz/m
}

// --- Physics ---

// resolveCollision performs 3D elastic sphere-sphere collision
func resolveCollision(a, b *Part) {
	if a.Frozen && b.Frozen {
		return
	}

	massA, massB := a.Mass, b.Mass
	if a.Frozen {
		massA = frozenCollisionMass
	}
	if b.Frozen {
		massB = frozenCollisionMass
	}

	if !physics.SeparateOverlap3D(&a.Pos, &b.Pos, a.Radius, b.Radius, massA, massB) {
		return
	}

	collided := physics.ElasticCollision3D(
		&a.Pos, &b.Pos,
		&a.Vel, &b.Vel,
		massA, massB, restitution,
	)
	if a.Frozen {
		a.Vel = vmath.Vec3F{}
	}
	if b.Frozen {
		b.Vel = vmath.Vec3F{}
	}
	if collided {
		a.Flash = flashSeconds
		b.Flash = flashSeconds
	}
}

// --- Projection ---

func projectPart(p *Part, idx, screenW, screenH int) projected {
	z := p.Pos.Z
	x := p.Pos.X
	y := p.Pos.Y
	r := p.Radius
	f := focalLen

	denom := z + f
	if denom < 0.5 {
		denom = 0.5
	}
	invZ := f / denom

	viewH := float64(screenH - hudRows)
	scale := viewH * 0.13

	return projected{
		cx:     float64(screenW)/2.0 + x*invZ*scale*2.0, // 2x for terminal cell aspect 1:2
		cy:     viewH/2.0 + y*invZ*scale,
		radius: r * invZ * scale,
		depth:  z,
		index:  idx,
	}
}

// --- Rendering ---

func renderSphere(buf *render.RenderBuffer, p *Part, proj projected, isSelected bool, screenW, viewH int) {
	if proj.radius < 0.4 {
		return
	}

	// Expand bounds for glow
	glowRadius := proj.radius * 1.6
	prX := glowRadius * 2.0
	prY := glowRadius

	minX := max(0, int(proj.cx-prX-1))
	maxX := min(screenW-1, int(proj.cx+prX+1))
	minY := max(0, int(proj.cy-prY-1))
	maxY := min(viewH-1, int(proj.cy+prY+1))

	// Neon: boost saturation, use depth for intensity not darkness
	zMin := boundsZMin
	zMax := boundsZMax
	depthT := (proj.depth - zMin) / (zMax - zMin)
	depthT = math.Max(0, math.Min(1, depthT))
	depthBright := 1.0 - depthT*0.4 // Less depth falloff

	// Saturated neon base
	baseR := math.Min(255, float64(p.Color.R)*1.3)
	baseG := math.Min(255, float64(p.Color.G)*1.3)
	baseB := math.Min(255, float64(p.Color.B)*1.3)

	flashT := 0.0
	if p.Flash > 0 {
		flashT = p.Flash / flashSeconds
	}

	sphereRadiusSq := 1.0
	coreRadius := 0.7 // Inner bright core

	for sy := minY; sy <= maxY; sy++ {
		for sx := minX; sx <= maxX; sx++ {
			// Use original sphere radius for core calculations
			nx := (float64(sx) + 0.5 - proj.cx) / (proj.radius * 2.0)
			ny := (float64(sy) + 0.5 - proj.cy) / proj.radius
			distSq := nx*nx + ny*ny

			// Glow extends beyond sphere
			if distSq > 2.5 {
				continue
			}

			var r, g, b float64

			if distSq <= sphereRadiusSq {
				// Inside sphere - neon core with hot center
				nz := math.Sqrt(1.0 - distSq)

				// Rim glow - strong colored edge
				rim := 1.0 - nz
				rim = rim * rim * 0.8

				// Core glow - white hot center
				coreDist := math.Sqrt(distSq) / coreRadius
				coreGlow := 0.0
				if coreDist < 1.0 {
					coreGlow = (1.0 - coreDist) * 0.6
				}

				// Specular hotspot
				spec := nx*halfX + ny*halfY + nz*halfZ
				if spec < 0 {
					spec = 0
				}
				spec = math.Pow(spec, 20.0) * 0.9

				// Combine: base color + rim tint + core white + specular
				intensity := (0.4 + rim*0.6) * depthBright
				r = baseR*intensity + coreGlow*255 + spec*255
				g = baseG*intensity + coreGlow*255 + spec*255
				b = baseB*intensity + coreGlow*255 + spec*255

			} else {
				// Outer glow - exponential falloff
				glowDist := math.Sqrt(distSq) - 1.0
				glowFalloff := math.Exp(-glowDist*3.0) * 0.5 * depthBright
				r = baseR * glowFalloff
				g = baseG * glowFalloff
				b = baseB * glowFalloff
			}

			// Frozen: cyan tint instead of grayscale
			if p.Frozen {
				avg := (r + g + b) / 3
				r = avg * 0.5
				g = avg*0.8 + 40
				b = avg + 60
			}

			// Flash: bright white pulse
			if flashT > 0 {
				flash := flashT * 0.8
				r = r*(1-flash) + 255*flash
				g = g*(1-flash) + 255*flash
				b = b*(1-flash) + 255*flash
			}

			// Selection: pulsing outer ring
			if isSelected && distSq > 0.8 && distSq <= 1.2 {
				pulse := 0.5 + 0.5*math.Sin(float64(time.Now().UnixMilli())/100.0)
				r = math.Min(255, r+80*pulse)
				g = math.Min(255, g+80*pulse)
				b = math.Min(255, b+40*pulse)
			}

			c := color.RGB{R: clampF(r), G: clampF(g), B: clampF(b)}

			// Alpha: solid core, fading glow
			alpha := 1.0
			if distSq > sphereRadiusSq {
				alpha = math.Max(0, 1.0-((math.Sqrt(distSq)-1.0)/0.6))
			} else {
				edgeDist := 1.0 - math.Sqrt(distSq)
				if edgeDist < 0.08 {
					alpha = edgeDist / 0.08
				}
			}

			// Use screen blend for additive glow effect
			if distSq > sphereRadiusSq {
				buf.Set(sx, sy, ' ', color.RGB{}, c, render.BlendScreen, alpha*0.7, terminal.AttrNone)
			} else {
				buf.Set(sx, sy, ' ', color.RGB{}, c, render.BlendAlpha, alpha, terminal.AttrNone)
			}
		}
	}
}

func renderFrame(buf *render.RenderBuffer, parts *[3]Part, selected, screenW, screenH int, paused bool) {
	viewH := screenH - hudRows

	// Project all parts
	projs := [3]projected{}
	for i := range parts {
		projs[i] = projectPart(&parts[i], i, screenW, screenH)
	}

	// Painter's algorithm: sort far to near
	order := [3]int{0, 1, 2}
	sort.Slice(order[:], func(i, j int) bool {
		return projs[order[i]].depth > projs[order[j]].depth
	})

	for _, idx := range order {
		renderSphere(buf, &parts[idx], projs[idx], idx == selected, screenW, viewH)
	}

	renderHUD(buf, parts, selected, screenW, screenH, paused)
}

func renderHUD(buf *render.RenderBuffer, parts *[3]Part, selected, screenW, screenH int, paused bool) {
	statusY := screenH - 2
	controlY := screenH - 1
	dim := color.RGB{R: 100, G: 100, B: 110}

	x := 1
	for i := range parts {
		marker := "  "
		if i == selected {
			marker = "> "
		}
		frozen := ""
		if parts[i].Frozen {
			frozen = " [F]"
		}
		s := fmt.Sprintf("%sPart%d m=%.1f%s", marker, i+1, parts[i].Mass, frozen)

		fg := parts[i].Color
		if parts[i].Frozen {
			fg = color.Lerp(fg, color.Grayscale(fg), 0.5)
		}
		writeStr(buf, x, statusY, s, fg)
		x += len([]rune(s)) + 3
	}

	if paused {
		writeStr(buf, screenW-9, statusY, "[PAUSED]", color.RGB{R: 255, G: 200, B: 50})
	}

	writeStr(buf, 1, controlY, "1/2/3:sel  f:freeze  up/dn:mass  space:pause  r:reset  q:quit", dim)
}

func writeStr(buf *render.RenderBuffer, x, y int, s string, fg color.RGB) {
	for _, r := range s {
		buf.SetFgOnly(x, y, r, fg, terminal.AttrNone)
		x++
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

// --- Main ---

func main() {
	initLighting()

	term := terminal.New(terminal.ColorModeTrueColor)
	if err := term.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal init: %v\n", err)
		os.Exit(1)
	}
	defer term.Fini()

	w, h := term.Size()

	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, w, h)

	parts := initParts()
	selected := 0
	paused := false

	ticker := time.NewTicker(framePeriod)
	defer ticker.Stop()

	lastTick := time.Now()
	running := true

	// use channel-based input
	inputCh := startInputReader(term)

	for running {
		select {
		case <-ticker.C:
			// Drain input non-blocking
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
						buf.Resize(w, h)
						continue drainInput
					}
					switch {
					case ev.Key == terminal.KeyRune && ev.Rune == 'q':
						running = false
					case ev.Key == terminal.KeyRune && ev.Rune == '1':
						selected = 0
					case ev.Key == terminal.KeyRune && ev.Rune == '2':
						selected = 1
					case ev.Key == terminal.KeyRune && ev.Rune == '3':
						selected = 2
					case ev.Key == terminal.KeyRune && ev.Rune == 'f':
						parts[selected].Frozen = !parts[selected].Frozen
						if parts[selected].Frozen {
							parts[selected].Vel = vmath.Vec3F{}
						}
					case ev.Key == terminal.KeyUp:
						parts[selected].Mass += massStep
						if parts[selected].Mass > massMax {
							parts[selected].Mass = massMax
						}
					case ev.Key == terminal.KeyDown:
						parts[selected].Mass -= massStep
						if parts[selected].Mass < massMin {
							parts[selected].Mass = massMin
						}
					case ev.Key == terminal.KeyRune && ev.Rune == ' ':
						paused = !paused
					case ev.Key == terminal.KeyRune && ev.Rune == 'r':
						parts = initParts()
						selected = 0
						paused = false
					case ev.Key == terminal.KeyEscape:
						running = false
					}
				default:
					break drainInput
				}
			}

			// Tick
			now := time.Now()
			dtSec := now.Sub(lastTick).Seconds()
			lastTick = now
			if dtSec > 0.1 {
				dtSec = 0.1
			}
			if !paused {
				simulate(&parts, dtSec)
			}

			// Render
			buf.Clear()
			renderFrame(buf, &parts, selected, w, h, paused)
			buf.FlushToTerminal(term)
		}
	}
}

func initParts() [3]Part {
	return [3]Part{
		{
			Pos:    vmath.Vec3F{X: -4.0, Y: -2.0, Z: 10.0},
			Vel:    vmath.Vec3F{X: 5.0, Y: 2.0, Z: -3.0},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  color.RGB{R: 40, G: 180, B: 255}, // Cyan
		},
		{
			Pos:    vmath.Vec3F{X: 3.0, Y: 1.5, Z: 18.0},
			Vel:    vmath.Vec3F{X: -3.0, Y: -4.0, Z: 4.0},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  color.RGB{R: 255, G: 60, B: 120}, // Magenta
		},
		{
			Pos:    vmath.Vec3F{X: 0.0, Y: 0.0, Z: 24.0},
			Vel:    vmath.Vec3F{X: 2.0, Y: 3.5, Z: -6.0},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  color.RGB{R: 120, G: 255, B: 80}, // Lime
		},
	}
}

func simulate(parts *[3]Part, dt float64) {
	// Integrate positions
	for i := range parts {
		if parts[i].Frozen {
			continue
		}
		parts[i].Pos = vmath.V3FAdd(parts[i].Pos, vmath.V3FScale(parts[i].Vel, dt))
	}

	// Boundary reflection per axis
	for i := range parts {
		if parts[i].Frozen {
			continue
		}
		physics.ReflectAxis3D(&parts[i].Pos.X, &parts[i].Vel.X, -boundsX, boundsX, restitution)
		physics.ReflectAxis3D(&parts[i].Pos.Y, &parts[i].Vel.Y, -boundsY, boundsY, restitution)
		physics.ReflectAxis3D(&parts[i].Pos.Z, &parts[i].Vel.Z, boundsZMin, boundsZMax, restitution)
	}

	// Pair-wise sphere collisions
	resolveCollision(&parts[0], &parts[1])
	resolveCollision(&parts[0], &parts[2])
	resolveCollision(&parts[1], &parts[2])

	// Decay flash timers
	for i := range parts {
		if parts[i].Flash > 0 {
			parts[i].Flash -= dt
			if parts[i].Flash < 0 {
				parts[i].Flash = 0
			}
		}
	}
}

func startInputReader(term terminal.Terminal) chan terminal.Event {
	ch := make(chan terminal.Event, 64)
	go func() {
		for {
			ev := term.PollEvent()
			select {
			case ch <- ev:
			default:
			}
			if ev.Type == terminal.EventClosed || ev.Type == terminal.EventError {
				close(ch)
				return
			}
		}
	}()
	return ch
}
