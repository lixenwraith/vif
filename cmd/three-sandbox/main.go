package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"time"

	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// Vec3 is a 3D vector in Q32.32
type Vec3 struct {
	X, Y, Z int64
}

// Part represents one composite sphere entity
type Part struct {
	Pos, Vel Vec3
	Mass     int64 // Q32.32
	Radius   int64 // Q32.32
	Color    terminal.RGB
	Frozen   bool
	Flash    int64 // Q32.32 remaining flash seconds
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
)

var (
	boundsX    = vmath.FromFloat(16.0)
	boundsY    = vmath.FromFloat(8.0)
	boundsZMin = vmath.FromFloat(3.0)
	boundsZMax = vmath.FromFloat(32.0)

	focalLen    = vmath.FromFloat(14.0)
	restitution = vmath.FromFloat(0.8)
	partRadius  = vmath.FromFloat(2.8)
	massDefault = vmath.FromFloat(5.0)
	massStep    = vmath.FromFloat(0.5)
	massMin     = vmath.FromFloat(0.1)
	massMax     = vmath.FromFloat(20.0)
	flashDur    = vmath.FromFloat(flashSeconds)

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

// --- Vec3 operations using vmath primitives ---

func v3Sub(a, b Vec3) Vec3 {
	return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z}
}

func v3Add(a, b Vec3) Vec3 {
	return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func v3Scale(v Vec3, s int64) Vec3 {
	return Vec3{vmath.Mul(v.X, s), vmath.Mul(v.Y, s), vmath.Mul(v.Z, s)}
}

func v3Dot(a, b Vec3) int64 {
	return vmath.Mul(a.X, b.X) + vmath.Mul(a.Y, b.Y) + vmath.Mul(a.Z, b.Z)
}

func v3MagSq(v Vec3) int64 {
	return vmath.Mul(v.X, v.X) + vmath.Mul(v.Y, v.Y) + vmath.Mul(v.Z, v.Z)
}

func v3Mag(v Vec3) int64 {
	return vmath.Sqrt(v3MagSq(v))
}

func v3Normalize(v Vec3) Vec3 {
	m := v3Mag(v)
	if m == 0 {
		return Vec3{}
	}
	return Vec3{vmath.Div(v.X, m), vmath.Div(v.Y, m), vmath.Div(v.Z, m)}
}

// --- Physics ---

// reflectAxis clamps position and reflects velocity on boundary contact
func reflectAxis(pos, vel *int64, lo, hi, e int64) {
	if *pos < lo {
		*pos = lo
		if *vel < 0 {
			*vel = -vmath.Mul(*vel, e)
		}
	} else if *pos > hi {
		*pos = hi
		if *vel > 0 {
			*vel = -vmath.Mul(*vel, e)
		}
	}
}

// resolveCollision performs 3D elastic sphere-sphere collision
func resolveCollision(a, b *Part) {
	if a.Frozen && b.Frozen {
		return
	}

	delta := v3Sub(b.Pos, a.Pos)
	dist := v3Mag(delta)
	minDist := a.Radius + b.Radius

	if dist >= minDist || dist == 0 {
		return
	}

	// Collision normal from a toward b
	n := Vec3{
		vmath.Div(delta.X, dist),
		vmath.Div(delta.Y, dist),
		vmath.Div(delta.Z, dist),
	}

	// Separate overlap unconditionally
	overlap := minDist - dist
	separateParts(a, b, n, overlap)

	// Impulse only if approaching
	relVel := v3Sub(a.Vel, b.Vel)
	vn := v3Dot(relVel, n)
	if vn <= 0 {
		return
	}

	// Inverse masses (frozen = infinite mass → zero inverse)
	var invA, invB int64
	if !a.Frozen {
		invA = vmath.Div(vmath.Scale, a.Mass)
	}
	if !b.Frozen {
		invB = vmath.Div(vmath.Scale, b.Mass)
	}
	invSum := invA + invB
	if invSum == 0 {
		return
	}

	// j = (1 + e) * vn / (1/mA + 1/mB)
	j := vmath.Div(vmath.Mul(vmath.Scale+restitution, vn), invSum)

	if !a.Frozen {
		a.Vel = v3Sub(a.Vel, v3Scale(n, vmath.Mul(j, invA)))
	}
	if !b.Frozen {
		b.Vel = v3Add(b.Vel, v3Scale(n, vmath.Mul(j, invB)))
	}

	a.Flash = flashDur
	b.Flash = flashDur
}

func separateParts(a, b *Part, n Vec3, overlap int64) {
	if overlap <= 0 {
		return
	}
	margin := int64(vmath.Scale / 16)

	if a.Frozen {
		b.Pos = v3Add(b.Pos, v3Scale(n, overlap+margin))
	} else if b.Frozen {
		a.Pos = v3Sub(a.Pos, v3Scale(n, overlap+margin))
	} else {
		half := overlap/2 + margin
		a.Pos = v3Sub(a.Pos, v3Scale(n, half))
		b.Pos = v3Add(b.Pos, v3Scale(n, half))
	}
}

// --- Projection ---

func projectPart(p *Part, idx, screenW, screenH int) projected {
	z := vmath.ToFloat(p.Pos.Z)
	x := vmath.ToFloat(p.Pos.X)
	y := vmath.ToFloat(p.Pos.Y)
	r := vmath.ToFloat(p.Radius)
	f := vmath.ToFloat(focalLen)

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
	zMin := vmath.ToFloat(boundsZMin)
	zMax := vmath.ToFloat(boundsZMax)
	depthT := (proj.depth - zMin) / (zMax - zMin)
	depthT = math.Max(0, math.Min(1, depthT))
	depthBright := 1.0 - depthT*0.4 // Less depth falloff

	// Saturated neon base
	baseR := math.Min(255, float64(p.Color.R)*1.3)
	baseG := math.Min(255, float64(p.Color.G)*1.3)
	baseB := math.Min(255, float64(p.Color.B)*1.3)

	flashT := 0.0
	if p.Flash > 0 {
		flashT = vmath.ToFloat(p.Flash) / flashSeconds
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

			color := terminal.RGB{R: clampF(r), G: clampF(g), B: clampF(b)}

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
				buf.Set(sx, sy, ' ', terminal.RGB{}, color, render.BlendScreen, alpha*0.7, terminal.AttrNone)
			} else {
				buf.Set(sx, sy, ' ', terminal.RGB{}, color, render.BlendAlpha, alpha, terminal.AttrNone)
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
	dim := terminal.RGB{R: 100, G: 100, B: 110}

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
		s := fmt.Sprintf("%sPart%d m=%.1f%s", marker, i+1, vmath.ToFloat(parts[i].Mass), frozen)

		fg := parts[i].Color
		if parts[i].Frozen {
			fg = render.Lerp(fg, render.Grayscale(fg), 0.5)
		}
		writeStr(buf, x, statusY, s, fg)
		x += len([]rune(s)) + 3
	}

	if paused {
		writeStr(buf, screenW-9, statusY, "[PAUSED]", terminal.RGB{R: 255, G: 200, B: 50})
	}

	writeStr(buf, 1, controlY, "1/2/3:sel  f:freeze  up/dn:mass  space:pause  r:reset  q:quit", dim)
}

func writeStr(buf *render.RenderBuffer, x, y int, s string, fg terminal.RGB) {
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

	// CHANGED: use channel-based input
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
							parts[selected].Vel = Vec3{}
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
			dt := vmath.FromFloat(dtSec)

			if !paused {
				simulate(&parts, dt)
			}

			// Render
			buf.Clear()
			renderFrame(buf, &parts, selected, w, h, paused)
			buf.FlushToTerminal(term)
		}
	}
}

// func initParts() [3]Part {
// 	return [3]Part{
// 		{
// 			Pos:    Vec3{vmath.FromFloat(-4.0), vmath.FromFloat(-2.0), vmath.FromFloat(10.0)},
// 			Vel:    Vec3{vmath.FromFloat(5.0), vmath.FromFloat(2.0), vmath.FromFloat(-3.0)},
// 			Mass:   massDefault,
// 			Radius: partRadius,
// 			Color:  terminal.RGB{R: 80, G: 160, B: 255}, // Blue
// 		},
// 		{
// 			Pos:    Vec3{vmath.FromFloat(3.0), vmath.FromFloat(1.5), vmath.FromFloat(18.0)},
// 			Vel:    Vec3{vmath.FromFloat(-3.0), vmath.FromFloat(-4.0), vmath.FromFloat(4.0)},
// 			Mass:   massDefault,
// 			Radius: partRadius,
// 			Color:  terminal.RGB{R: 255, G: 90, B: 90}, // Red
// 		},
// 		{
// 			Pos:    Vec3{vmath.FromFloat(0.0), vmath.FromFloat(0.0), vmath.FromFloat(24.0)},
// 			Vel:    Vec3{vmath.FromFloat(2.0), vmath.FromFloat(3.5), vmath.FromFloat(-6.0)},
// 			Mass:   massDefault,
// 			Radius: partRadius,
// 			Color:  terminal.RGB{R: 90, G: 255, B: 120}, // Green
// 		},
// 	}
// }

func initParts() [3]Part {
	return [3]Part{
		{
			Pos:    Vec3{vmath.FromFloat(-4.0), vmath.FromFloat(-2.0), vmath.FromFloat(10.0)},
			Vel:    Vec3{vmath.FromFloat(5.0), vmath.FromFloat(2.0), vmath.FromFloat(-3.0)},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  terminal.RGB{R: 40, G: 180, B: 255}, // Cyan
		},
		{
			Pos:    Vec3{vmath.FromFloat(3.0), vmath.FromFloat(1.5), vmath.FromFloat(18.0)},
			Vel:    Vec3{vmath.FromFloat(-3.0), vmath.FromFloat(-4.0), vmath.FromFloat(4.0)},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  terminal.RGB{R: 255, G: 60, B: 120}, // Magenta
		},
		{
			Pos:    Vec3{vmath.FromFloat(0.0), vmath.FromFloat(0.0), vmath.FromFloat(24.0)},
			Vel:    Vec3{vmath.FromFloat(2.0), vmath.FromFloat(3.5), vmath.FromFloat(-6.0)},
			Mass:   massDefault,
			Radius: partRadius,
			Color:  terminal.RGB{R: 120, G: 255, B: 80}, // Lime
		},
	}
}

func simulate(parts *[3]Part, dt int64) {
	// Integrate positions
	for i := range parts {
		if parts[i].Frozen {
			continue
		}
		parts[i].Pos = v3Add(parts[i].Pos, v3Scale(parts[i].Vel, dt))
	}

	// Boundary reflection per axis
	for i := range parts {
		if parts[i].Frozen {
			continue
		}
		reflectAxis(&parts[i].Pos.X, &parts[i].Vel.X, -boundsX, boundsX, restitution)
		reflectAxis(&parts[i].Pos.Y, &parts[i].Vel.Y, -boundsY, boundsY, restitution)
		reflectAxis(&parts[i].Pos.Z, &parts[i].Vel.Z, boundsZMin, boundsZMax, restitution)
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