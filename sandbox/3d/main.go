// Sandbox 3d draws solid objects into half-block pixels through a depth buffer, so
// overlap, silhouette lines, cast halos and shading gradients read as depth in text.
// The tribar is a real broken loop seen from the one angle that closes it.
package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

const (
	framePeriod = time.Second / 30
	panelW      = 25
	sceneKnobs  = 6
	strands     = 3.0
	tubeStep    = 0.3 // sphere spacing as a fraction of the tube radius
)

var (
	bgColor = color.RGB{R: 12, G: 12, B: 20}
	panelBg = color.RGB{R: 18, G: 18, B: 28}
	inf     = math.Inf(1)
)

// --- Vectors ---

type vec3 struct{ x, y, z float64 }

func (a vec3) add(b vec3) vec3      { return vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a vec3) sub(b vec3) vec3      { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) scale(s float64) vec3 { return vec3{a.x * s, a.y * s, a.z * s} }
func (a vec3) mul(b vec3) vec3      { return vec3{a.x * b.x, a.y * b.y, a.z * b.z} }
func (a vec3) dot(b vec3) float64   { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a vec3) len() float64         { return math.Sqrt(a.dot(a)) }

func (a vec3) cross(b vec3) vec3 {
	return vec3{a.y*b.z - a.z*b.y, a.z*b.x - a.x*b.z, a.x*b.y - a.y*b.x}
}

func (a vec3) norm() vec3 {
	l := a.len()
	if l < 1e-12 {
		return vec3{}
	}
	return a.scale(1 / l)
}

func (a vec3) axis(i int) float64 {
	switch i {
	case 0:
		return a.x
	case 1:
		return a.y
	}
	return a.z
}

func lerp3(a, b vec3, t float64) vec3 { return a.add(b.sub(a).scale(t)) }

// perp picks a unit vector orthogonal to t
func perp(t vec3) vec3 {
	a := vec3{0, 0, 1}
	if math.Abs(t.z) > 0.9 {
		a = vec3{1, 0, 0}
	}
	return a.sub(t.scale(a.dot(t))).norm()
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func rgbVec(c color.RGB) vec3 {
	return vec3{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255}
}

func vecRGB(v vec3) color.RGB {
	return color.RGB{R: uint8(clamp01(v.x)*255 + 0.5), G: uint8(clamp01(v.y)*255 + 0.5), B: uint8(clamp01(v.z)*255 + 0.5)}
}

// mat3 rows are the camera axes (right, up, back) in object space: mul takes object
// space to camera space, tmul takes camera space back.
type mat3 [3]vec3

func (m mat3) mul(v vec3) vec3  { return vec3{m[0].dot(v), m[1].dot(v), m[2].dot(v)} }
func (m mat3) tmul(v vec3) vec3 { return m[0].scale(v.x).add(m[1].scale(v.y)).add(m[2].scale(v.z)) }

func (m mat3) mulM(n mat3) mat3 {
	var r mat3
	for i := range 3 {
		r[i] = n[0].scale(m[i].x).add(n[1].scale(m[i].y)).add(n[2].scale(m[i].z))
	}
	return r
}

func rotX(a float64) mat3 {
	c, s := math.Cos(a), math.Sin(a)
	return mat3{{1, 0, 0}, {0, c, -s}, {0, s, c}}
}

func rotY(a float64) mat3 {
	c, s := math.Cos(a), math.Sin(a)
	return mat3{{c, 0, s}, {0, 1, 0}, {-s, 0, c}}
}

// lookBasis is the camera basis for a viewer out along from, with up as the screen's up hint
func lookBasis(from, up vec3) mat3 {
	back := from.norm()
	u := up.sub(back.scale(up.dot(back))).norm()
	return mat3{u.cross(back), u, back}
}

// --- Primitives ---

type material struct {
	a, b color.RGB // base, and the hue it drifts toward along the curve
	spec float64
}

func (m *material) at(u, drift float64) vec3 {
	return lerp3(rgbVec(m.a), rgbVec(m.b), drift*(0.5-0.5*math.Cos(2*math.Pi*u)))
}

// sphere is one splat of a tube; a zero tangent makes it a plain ball with its own normal
type sphere struct {
	c    vec3
	r    float64
	t, n vec3    // unit tangent and transported frame normal along the curve
	u    float64 // 0..1 along the curve
	ph   float64 // strand phase at u, continuous around a closed loop
	mat  int
}

type box struct {
	lo, hi vec3
	axis   int     // object axis the bar runs along
	u0, u1 float64 // u at lo and hi on that axis
	mat    int
}

// tube splats spheres along a polyline, carrying a parallel-transported frame whose closure
// error is spread over a loop so strands meet themselves; sharp bends and open ends get balls.
func tube(out []sphere, pts []vec3, closed bool, r, twist float64, mat int) []sphere {
	if closed {
		pts = append(append([]vec3(nil), pts...), pts[0])
	}
	n := len(pts)
	if n < 2 {
		return out
	}
	dirs := make([]vec3, n)
	segs := make([]float64, n)
	total := 0.0
	for i := 1; i < n; i++ {
		d := pts[i].sub(pts[i-1])
		segs[i] = d.len()
		dirs[i] = d.norm()
		total += segs[i]
	}
	if total < 1e-9 {
		return out
	}
	frames := make([]vec3, n)
	frames[1] = perp(dirs[1])
	for i := 2; i < n; i++ {
		f := frames[i-1].sub(dirs[i].scale(frames[i-1].dot(dirs[i]))).norm()
		if f == (vec3{}) {
			f = perp(dirs[i])
		}
		frames[i] = f
	}
	holonomy := 0.0
	if closed {
		e := frames[n-1].sub(dirs[1].scale(frames[n-1].dot(dirs[1]))).norm()
		holonomy = math.Atan2(e.dot(dirs[1].cross(frames[1])), e.dot(frames[1]))
	}
	turns := math.Round(twist * total)
	spacing := r * tubeStep
	s, next := 0.0, 0.0
	for i := 1; i < n; i++ {
		if segs[i] < 1e-9 {
			continue
		}
		if (i == 1 && !closed) || (i > 1 && dirs[i-1].dot(dirs[i]) < 0.95) {
			out = append(out, sphere{c: pts[i-1], r: r, u: s / total, mat: mat})
		}
		for next <= s+segs[i] {
			u := next / total
			a := -holonomy * u
			fr := frames[i].scale(math.Cos(a)).add(dirs[i].cross(frames[i]).scale(math.Sin(a)))
			out = append(out, sphere{
				c: pts[i-1].add(dirs[i].scale(next - s)), r: r, t: dirs[i], n: fr,
				u: u, ph: 2 * math.Pi * turns * u, mat: mat,
			})
			next += spacing
		}
		s += segs[i]
	}
	if !closed {
		out = append(out, sphere{c: pts[n-1], r: r, u: 1, mat: mat})
	}
	return out
}

// --- Scenes ---

type scene struct {
	name   string
	base   mat3
	radius float64 // framing radius in object units
	mats   []material
	build  func(s *scene) ([]sphere, []box)

	spin, wobble, yaw, pitch, thick, twist float64
}

const tribarL = 10.0

var (
	diagonalView = lookBasis(vec3{1, 1, 1}, vec3{0, 0, 1})
	// tribarOff centers the figure in projection and in depth along the viewing diagonal
	tribarOff = vec3{-5 * tribarL / 6, -tribarL / 2, -tribarL / 6}
	hemp      = material{a: color.RGB{R: 206, G: 166, B: 106}, b: color.RGB{R: 150, G: 112, B: 92}, spec: 0.3}
)

// tribarPath is the open loop +x, +y, +z whose two ends coincide when seen along (1,1,1)
func tribarPath() []vec3 {
	return []vec3{
		tribarOff,
		vec3{tribarL, 0, 0}.add(tribarOff),
		vec3{tribarL, tribarL, 0}.add(tribarOff),
		vec3{tribarL, tribarL, tribarL}.add(tribarOff),
	}
}

func newTribar(rope bool) scene {
	s := scene{
		name: "tribar", base: diagonalView, radius: 8, wobble: 18, thick: 1,
		mats: []material{{a: color.RGB{R: 228, G: 198, B: 150}, b: color.RGB{R: 150, G: 170, B: 230}, spec: 0.25}},
	}
	if rope {
		s.name, s.thick, s.twist, s.mats = "tribar rope", 0.9, 0.25, []material{hemp}
		s.build = func(s *scene) ([]sphere, []box) {
			return tube(nil, tribarPath(), false, s.thick, s.twist, 0), nil
		}
		return s
	}
	s.build = func(s *scene) ([]sphere, []box) {
		p, hw, L := tribarPath(), s.thick, tribarL
		third := func(k, v float64) float64 { return (k + v/L) / 3 }
		return nil, []box{
			{lo: p[0].sub(vec3{hw, hw, hw}), hi: p[1].add(vec3{hw, hw, hw}), axis: 0, u0: third(0, -hw), u1: third(1, hw)},
			{lo: p[1].sub(vec3{hw, hw, hw}), hi: p[2].add(vec3{hw, hw, hw}), axis: 1, u0: third(1, -hw), u1: third(2, hw)},
			{lo: p[2].sub(vec3{hw, hw, hw}), hi: p[3].add(vec3{hw, hw, hw}), axis: 2, u0: third(2, -hw), u1: third(3, hw)},
		}
	}
	return s
}

// newRings is the three mutually perpendicular golden ellipses: each passes over one
// neighbour and under the other, and no two are linked on their own.
func newRings() scene {
	return scene{
		name: "rings", base: diagonalView, radius: 11, spin: 12, thick: 1.1,
		mats: []material{
			{a: color.RGB{R: 232, G: 92, B: 84}, b: color.RGB{R: 255, G: 170, B: 120}, spec: 0.5},
			{a: color.RGB{R: 84, G: 196, B: 124}, b: color.RGB{R: 170, G: 240, B: 160}, spec: 0.5},
			{a: color.RGB{R: 92, G: 140, B: 240}, b: color.RGB{R: 160, G: 200, B: 255}, spec: 0.5},
		},
		build: func(s *scene) ([]sphere, []box) {
			const a, b, n = 9.7, 6.0, 96
			var out []sphere
			for ring := range 3 {
				pts := make([]vec3, n)
				for i := range pts {
					t := 2 * math.Pi * float64(i) / n
					p, q := a*math.Cos(t), b*math.Sin(t)
					switch ring {
					case 0:
						pts[i] = vec3{p, q, 0}
					case 1:
						pts[i] = vec3{0, p, q}
					default:
						pts[i] = vec3{q, 0, p}
					}
				}
				out = tube(out, pts, true, s.thick, s.twist, ring)
			}
			return out, nil
		},
	}
}

func newKnot() scene {
	return scene{
		name: "trefoil rope", base: lookBasis(vec3{0, -1, 1.4}, vec3{0, 0, 1}), radius: 10,
		spin: 10, thick: 0.75, twist: 0.25, mats: []material{hemp},
		build: func(s *scene) ([]sphere, []box) {
			const n = 240
			pts := make([]vec3, n)
			for i := range pts {
				t := 2 * math.Pi * float64(i) / n
				pts[i] = vec3{math.Sin(t) + 2*math.Sin(2*t), math.Cos(t) - 2*math.Cos(2*t), -math.Sin(3 * t)}.scale(3)
			}
			return tube(nil, pts, true, s.thick, s.twist, 0), nil
		},
	}
}

// newChain closes stadium links around a circle, alternating flat and upright so each
// threads the next through its eye.
func newChain() scene {
	return scene{
		name: "chain", base: lookBasis(vec3{0, -1, 1.4}, vec3{0, 0, 1}), radius: 10,
		spin: 12, thick: 0.45,
		mats: []material{
			{a: color.RGB{R: 200, G: 205, B: 215}, b: color.RGB{R: 150, G: 160, B: 180}, spec: 1.2},
			{a: color.RGB{R: 236, G: 196, B: 96}, b: color.RGB{R: 190, G: 140, B: 70}, spec: 1.2},
		},
		build: func(s *scene) ([]sphere, []box) {
			const links, straight, rho, pitch = 14, 3.0, 1.2, 3.6
			circleR := links * pitch / (2 * math.Pi)
			var out []sphere
			for i := range links {
				phi := 2 * math.Pi * float64(i) / links
				radial := vec3{math.Cos(phi), math.Sin(phi), 0}
				along := vec3{-math.Sin(phi), math.Cos(phi), 0}
				across := vec3{0, 0, 1}
				if i%2 == 0 {
					across = radial
				}
				center := radial.scale(circleR)
				var pts []vec3
				put := func(x, y float64) {
					pts = append(pts, center.add(along.scale(x)).add(across.scale(y)))
				}
				for k := range 12 {
					put(-straight/2+straight*float64(k)/12, rho)
				}
				for k := range 16 {
					a := math.Pi/2 - math.Pi*float64(k)/16
					put(straight/2+rho*math.Cos(a), rho*math.Sin(a))
				}
				for k := range 12 {
					put(straight/2-straight*float64(k)/12, -rho)
				}
				for k := range 16 {
					a := -math.Pi/2 - math.Pi*float64(k)/16
					put(-straight/2+rho*math.Cos(a), rho*math.Sin(a))
				}
				out = tube(out, pts, true, s.thick, s.twist, i%2)
			}
			return out, nil
		},
	}
}

func makeScene(i int) scene {
	switch i {
	case 1:
		return newTribar(true)
	case 2:
		return newRings()
	case 3:
		return newKnot()
	case 4:
		return newChain()
	}
	return newTribar(false)
}

// --- Camera and buffers ---

type renderKnobs struct {
	persp, zoom, aspect                     float64
	lightAz, lightEl                        float64
	ambient, specular, skyLight, rimDark    float64
	edgeLine, halo, haloSize, fog, hueDrift float64
}

func defaultRender() renderKnobs {
	return renderKnobs{
		zoom: 1, aspect: 2, lightAz: 125, lightEl: 40,
		ambient: 0.3, specular: 0.6, skyLight: 0.5, rimDark: 0.35,
		edgeLine: 0.45, halo: 0.55, haloSize: 3, fog: 0.2, hueDrift: 0.3,
	}
}

type camera struct {
	v              mat3
	ortho          bool
	eye            float64 // perspective eye distance from the origin
	sx, sy, cx, cy float64
	oz             float64 // orthographic ray origin height
	oObj, dObj     vec3    // perspective origin and orthographic direction, in object space
}

func newCamera(v mat3, rk *renderKnobs, radius float64, pw, ph int) camera {
	c := camera{v: v}
	fit := math.Min(float64(pw-panelW), float64(ph)*rk.aspect/2)
	c.sx = rk.zoom * fit / (2.2 * radius)
	c.sy = c.sx * 2 / rk.aspect
	c.cx = float64(panelW) + float64(pw-panelW)/2
	c.cy = float64(ph) / 2
	c.oz = 4 * radius
	c.ortho = rk.persp < 1e-3
	c.eye = 1.6 * radius / math.Max(rk.persp, 1e-3)
	c.dObj = v[2].scale(-1)
	c.oObj = v[2].scale(c.eye)
	return c
}

// ray is the object-space ray through a pixel
func (c *camera) ray(px, py int) (o, d vec3) {
	x := (float64(px) + 0.5 - c.cx) / c.sx
	y := -(float64(py) + 0.5 - c.cy) / c.sy
	if c.ortho {
		return c.v.tmul(vec3{x, y, c.oz}), c.dObj
	}
	return c.oObj, c.v.tmul(vec3{x, y, -c.eye}.norm())
}

// project maps an object-space point to a pixel and the perspective scale there
func (c *camera) project(p vec3) (sx, sy, f float64, ok bool) {
	q := c.v.mul(p)
	f = 1
	if !c.ortho {
		dz := c.eye - q.z
		if dz < 0.2 {
			return 0, 0, 0, false
		}
		f = c.eye / dz
	}
	return c.cx + q.x*c.sx*f, c.cy - q.y*c.sy*f, f, true
}

func (c *camera) refDepth() float64 {
	if c.ortho {
		return c.oz
	}
	return c.eye
}

type gbuf struct {
	w, h   int
	depth  []float64
	nrm    []vec3 // camera space
	base   []vec3
	spec   []float64
	groove []float64
	col    []vec3
}

func newGbuf(w, h int) *gbuf {
	n := w * h
	return &gbuf{
		w: w, h: h, depth: make([]float64, n), nrm: make([]vec3, n), base: make([]vec3, n),
		spec: make([]float64, n), groove: make([]float64, n), col: make([]vec3, n),
	}
}

func (g *gbuf) clear() {
	for i := range g.depth {
		g.depth[i] = inf
	}
}

func (g *gbuf) put(idx int, c *camera, t float64, n, base vec3, spec, groove float64) {
	g.depth[idx] = t
	g.nrm[idx] = c.v.mul(n.norm())
	g.base[idx] = base
	g.spec[idx] = spec
	g.groove[idx] = groove
}

// splatSphere writes a tube sample; the normal comes from the local tangent so adjacent
// splats shade as one cylinder, and strands tilt it like a bump map.
func (g *gbuf) splatSphere(c *camera, s *sphere, m *material, twist, drift float64) {
	sx, sy, f, ok := c.project(s.c)
	if !ok {
		return
	}
	rx, ry := s.r*c.sx*f+1.5, s.r*c.sy*f+1.5
	x0, x1 := max(0, int(sx-rx)), min(g.w-1, int(sx+rx)+1)
	y0, y1 := max(0, int(sy-ry)), min(g.h-1, int(sy+ry)+1)
	base := m.at(s.u, drift)
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			o, d := c.ray(px, py)
			oc := o.sub(s.c)
			b := oc.dot(d)
			disc := b*b - oc.dot(oc) + s.r*s.r
			if disc < 0 {
				continue
			}
			t := -b - math.Sqrt(disc)
			idx := py*g.w + px
			if t <= 0 || t >= g.depth[idx] {
				continue
			}
			n := oc.add(d.scale(t))
			groove := 1.0
			if s.t != (vec3{}) {
				n = n.sub(s.t.scale(n.dot(s.t))).norm()
				if twist > 0 {
					bn := s.t.cross(s.n)
					th := math.Atan2(n.dot(bn), n.dot(s.n))
					ph := strands*th + s.ph
					tang := s.n.scale(-math.Sin(th)).add(bn.scale(math.Cos(th)))
					n = n.add(tang.scale(0.5 * math.Sin(ph)))
					groove = 1 - 0.45*(0.5-0.5*math.Cos(ph))
				}
			}
			g.put(idx, c, t, n, base, m.spec, groove)
		}
	}
}

func hitBox(o, d vec3, b *box) (t float64, n vec3, ok bool) {
	tmin, tmax := -inf, inf
	axis, sign := 0, 0.0
	for i := range 3 {
		oi, di, lo, hi := o.axis(i), d.axis(i), b.lo.axis(i), b.hi.axis(i)
		if math.Abs(di) < 1e-9 {
			if oi < lo || oi > hi {
				return 0, n, false
			}
			continue
		}
		t1, t2, ns := (lo-oi)/di, (hi-oi)/di, -1.0
		if t1 > t2 {
			t1, t2, ns = t2, t1, 1
		}
		if t1 > tmin {
			tmin, axis, sign = t1, i, ns
		}
		tmax = math.Min(tmax, t2)
		if tmin > tmax {
			return 0, n, false
		}
	}
	if tmin <= 0 {
		return 0, n, false
	}
	switch axis {
	case 0:
		n = vec3{sign, 0, 0}
	case 1:
		n = vec3{0, sign, 0}
	default:
		n = vec3{0, 0, sign}
	}
	return tmin, n, true
}

func (g *gbuf) splatBox(c *camera, b *box, m *material, drift float64) {
	x0, y0, x1, y1 := g.w, g.h, -1, -1
	for i := range 8 {
		p := b.lo
		if i&1 != 0 {
			p.x = b.hi.x
		}
		if i&2 != 0 {
			p.y = b.hi.y
		}
		if i&4 != 0 {
			p.z = b.hi.z
		}
		sx, sy, _, ok := c.project(p)
		if !ok {
			return
		}
		x0, x1 = min(x0, int(sx)-1), max(x1, int(sx)+1)
		y0, y1 = min(y0, int(sy)-1), max(y1, int(sy)+1)
	}
	x0, x1 = max(x0, 0), min(x1, g.w-1)
	y0, y1 = max(y0, 0), min(y1, g.h-1)
	span := b.hi.axis(b.axis) - b.lo.axis(b.axis)
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			o, d := c.ray(px, py)
			t, n, ok := hitBox(o, d, b)
			idx := py*g.w + px
			if !ok || t >= g.depth[idx] {
				continue
			}
			p := o.add(d.scale(t))
			u := b.u0 + (p.axis(b.axis)-b.lo.axis(b.axis))/span*(b.u1-b.u0)
			g.put(idx, c, t, n, m.at(u, drift), m.spec, 1)
		}
	}
}

// shade lights each pixel in camera space: hemispheric ambient, Lambert, Blinn specular,
// rim darkening toward the silhouette, strand grooves and depth fog.
func (g *gbuf) shade(rk *renderKnobs, radius, refDepth float64) {
	az, el := rk.lightAz*math.Pi/180, rk.lightEl*math.Pi/180
	light := vec3{math.Cos(el) * math.Cos(az), math.Cos(el) * math.Sin(az), math.Sin(el)}
	half := light.add(vec3{0, 0, 1}).norm()
	sky, ground, white := vec3{0.72, 0.84, 1}, vec3{0.5, 0.38, 0.3}, vec3{1, 1, 1}
	bg := rgbVec(bgColor)
	fogCol := bg.scale(0.6)
	for i, t := range g.depth {
		if t == inf {
			g.col[i] = bg
			continue
		}
		n := g.nrm[i]
		amb := lerp3(white, lerp3(ground, sky, 0.5+0.5*n.y), rk.skyLight).scale(rk.ambient)
		c := g.base[i].mul(amb.add(white.scale(0.9 * math.Max(0, n.dot(light)))))
		if sp := rk.specular * g.spec[i] * math.Pow(math.Max(0, n.dot(half)), 32); sp > 0 {
			c = c.add(lerp3(white, g.base[i], 0.3).scale(sp))
		}
		rim := 1 - math.Max(0, n.z)
		c = c.scale((1 - rk.rimDark*rim*rim) * g.groove[i])
		g.col[i] = lerp3(c, fogCol, rk.fog*clamp01(0.5+(t-refDepth)/(2*radius)))
	}
}

// edges darkens the near side of a depth step or crease as a line, and the far side,
// background included, as a halo that falls off over the window. A neighbour counts as
// a step only where it leaves the plane through this pixel, so a tilted face is not its
// own occluder, and each ramps in over a second eps so motion never flips it in a frame.
func (g *gbuf) edges(rk *renderKnobs, eps, sx, sy float64) {
	w, h := g.w, g.h
	k := int(rk.haloSize + 0.5)
	kx := (k + 1) / 2
	for y := range h {
		for x := range w {
			i := y*w + x
			d := g.depth[i]
			gx, gy := 0.0, 0.0
			if d != inf {
				n := g.nrm[i]
				nz := math.Max(n.z, 0.15)
				gx, gy = n.x/(nz*sx), -n.y/(nz*sy)
			}
			f := 1.0
			if d != inf && rk.edgeLine > 0 {
				line := 0.0
				for _, o := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					nx, ny := x+o[0], y+o[1]
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					j := ny*w + nx
					step := g.depth[j] - (d + gx*float64(o[0]) + gy*float64(o[1]))
					switch {
					case step > eps:
						line = math.Max(line, math.Min(1, step/eps-1))
					case math.Abs(step) <= eps && g.nrm[i].dot(g.nrm[j]) < 0.3:
						line = math.Max(line, 0.6)
					}
				}
				f *= 1 - rk.edgeLine*line
			}
			if k > 0 && rk.halo > 0 {
				occ := 0.0
				for dy := -k; dy <= k; dy++ {
					ny := y + dy
					if ny < 0 || ny >= h {
						continue
					}
					for dx := -kx; dx <= kx; dx++ {
						nx := x + dx
						if nx < 0 || nx >= w {
							continue
						}
						dj := g.depth[ny*w+nx]
						if dj == inf {
							continue
						}
						step := d + gx*float64(dx) + gy*float64(dy) - dj
						if step <= eps {
							continue
						}
						dist := math.Max(math.Abs(float64(dx))*2, math.Abs(float64(dy)))
						occ = math.Max(occ, (1-dist/float64(k+1))*math.Min(1, step/eps-1))
					}
				}
				f *= 1 - rk.halo*occ
			}
			if f < 1 {
				g.col[i] = g.col[i].scale(f)
			}
		}
	}
}

// blit packs two pixel rows into each cell row with an upper half block
func (g *gbuf) blit(cells []terminal.Cell, w, h int) {
	for y := range h {
		for x := range w {
			top, bot := vecRGB(g.col[2*y*w+x]), vecRGB(g.col[(2*y+1)*w+x])
			cell := terminal.Cell{Rune: ' ', Fg: top, Bg: bot}
			if top != bot {
				cell.Rune = '▀'
			}
			cells[y*w+x] = cell
		}
	}
}

func render(g *gbuf, sc *scene, rk *renderKnobs, now float64) {
	g.clear()
	yaw := (sc.yaw + sc.spin*now + sc.wobble*math.Sin(0.8*now)) * math.Pi / 180
	v := rotX(sc.pitch * math.Pi / 180).mulM(rotY(yaw)).mulM(sc.base)
	cam := newCamera(v, rk, sc.radius, g.w, g.h)
	spheres, boxes := sc.build(sc)
	for i := range spheres {
		g.splatSphere(&cam, &spheres[i], &sc.mats[spheres[i].mat], sc.twist, rk.hueDrift)
	}
	for i := range boxes {
		g.splatBox(&cam, &boxes[i], &sc.mats[boxes[i].mat], rk.hueDrift)
	}
	g.shade(rk, sc.radius, cam.refDepth())
	g.edges(rk, 0.5*sc.thick, cam.sx, cam.sy)
}

// --- Controls and HUD ---

type control struct {
	name           string
	value          *float64
	min, max, step float64
}

func buildControls(sc *scene, rk *renderKnobs) []control {
	return []control{
		{"Spin", &sc.spin, -60, 60, 2},
		{"Wobble", &sc.wobble, 0, 60, 2},
		{"Yaw", &sc.yaw, -180, 180, 5},
		{"Pitch", &sc.pitch, -90, 90, 5},
		{"Thickness", &sc.thick, 0.2, 2.5, 0.05},
		{"Twist", &sc.twist, 0, 1.5, 0.05},
		{"Persp", &rk.persp, 0, 1, 0.05},
		{"Zoom", &rk.zoom, 0.3, 3, 0.1},
		{"Aspect", &rk.aspect, 1.5, 2.6, 0.05},
		{"LightAz", &rk.lightAz, 0, 360, 10},
		{"LightEl", &rk.lightEl, 0, 90, 5},
		{"Ambient", &rk.ambient, 0, 1, 0.05},
		{"Specular", &rk.specular, 0, 2, 0.1},
		{"SkyLight", &rk.skyLight, 0, 1, 0.1},
		{"RimDark", &rk.rimDark, 0, 1, 0.05},
		{"EdgeLine", &rk.edgeLine, 0, 1, 0.05},
		{"Halo", &rk.halo, 0, 1, 0.05},
		{"HaloSize", &rk.haloSize, 0, 6, 1},
		{"Fog", &rk.fog, 0, 1, 0.05},
		{"HueDrift", &rk.hueDrift, 0, 1, 0.05},
	}
}

func drawHUD(cells []terminal.Cell, w, h int, sc *scene, ctrls []control, sel int, paused bool) {
	fg := color.RGB{R: 165, G: 165, B: 178}
	fgSel := color.RGB{R: 255, G: 220, B: 100}
	fgVal := color.RGB{R: 110, G: 230, B: 170}
	fgHead := color.RGB{R: 120, G: 135, B: 170}
	put := func(x, y int, s string, c color.RGB) {
		for _, r := range s {
			if x >= w || y >= h {
				return
			}
			cells[y*w+x] = terminal.Cell{Rune: r, Fg: c, Bg: panelBg}
			x++
		}
	}
	fill := func(y, width int) {
		for x := 0; x < width && x < w && y < h; x++ {
			cells[y*w+x] = terminal.Cell{Rune: ' ', Fg: fg, Bg: panelBg}
		}
	}
	y := 0
	fill(y, panelW)
	put(1, y, "3D SANDBOX", fgHead)
	put(12, y, sc.name, fgSel)
	y++
	fill(y, panelW)
	put(1, y, "1 tribar  2 rope  3 rings", fg)
	y++
	fill(y, panelW)
	put(1, y, "4 knot  5 chain", fg)
	if paused {
		put(17, y, "PAUSED", fgSel)
	}
	y += 2
	for i, c := range ctrls {
		if i == 0 || i == sceneKnobs {
			head := "scene"
			if i > 0 {
				head = "render"
			}
			fill(y, panelW)
			put(1, y, head, fgHead)
			y++
		}
		fill(y, panelW)
		marker, col, vc := "  ", fg, fg
		if i == sel {
			marker, col, vc = "> ", fgSel, fgVal
		}
		put(1, y, marker+c.name, col)
		put(16, y, fmt.Sprintf("%7.2f", *c.value), vc)
		y++
	}
	fill(h-1, w)
	put(1, h-1, "[W/S] Nav  [A/D] Adj  [1-5] Scene  [Space] Pause  [R] Reset  [Q] Quit", fg)
}

func startInputReader(term terminal.Terminal) <-chan terminal.Event {
	ch := make(chan terminal.Event, 64)
	go func() {
		defer close(ch)
		for {
			ev := term.PollEvent()
			switch ev.Type {
			case terminal.EventKey, terminal.EventResize:
				ch <- ev
			case terminal.EventClosed, terminal.EventError:
				return
			}
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
	g := newGbuf(w, 2*h)

	scenes := make([]scene, 5)
	for i := range scenes {
		scenes[i] = makeScene(i)
	}
	cur := 0
	rk := defaultRender()
	ctrls := buildControls(&scenes[cur], &rk)
	sel := 0
	paused := false
	now := 0.0

	inputCh := startInputReader(term)
	last := time.Now()
	running := true
	for running {
		frameStart := time.Now()
		dt := math.Min(frameStart.Sub(last).Seconds(), 0.1)
		last = frameStart

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
					g = newGbuf(w, 2*h)
					continue
				}
				r := ev.Rune
				if r >= 'A' && r <= 'Z' {
					r += 'a' - 'A'
				}
				switch {
				case ev.Key == terminal.KeyEscape, ev.Key == terminal.KeyRune && r == 'q':
					running = false
				case ev.Key == terminal.KeyRune && r >= '1' && r <= '5':
					cur = int(r - '1')
					ctrls = buildControls(&scenes[cur], &rk)
				case ev.Key == terminal.KeyRune && r == ' ':
					paused = !paused
				case ev.Key == terminal.KeyRune && r == 'r':
					scenes[cur] = makeScene(cur)
					rk = defaultRender()
				case ev.Key == terminal.KeyUp, ev.Key == terminal.KeyRune && r == 'w':
					sel = (sel + len(ctrls) - 1) % len(ctrls)
				case ev.Key == terminal.KeyDown, ev.Key == terminal.KeyRune && r == 's':
					sel = (sel + 1) % len(ctrls)
				case ev.Key == terminal.KeyLeft, ev.Key == terminal.KeyRune && r == 'a':
					c := &ctrls[sel]
					*c.value = math.Max(c.min, *c.value-c.step)
				case ev.Key == terminal.KeyRight, ev.Key == terminal.KeyRune && r == 'd':
					c := &ctrls[sel]
					*c.value = math.Min(c.max, *c.value+c.step)
				}
			default:
				break drainInput
			}
		}

		if !paused {
			now += dt
		}
		render(g, &scenes[cur], &rk, now)
		g.blit(cells, w, h)
		drawHUD(cells, w, h, &scenes[cur], ctrls, sel, paused)
		term.Flush(cells, w, h)

		if elapsed := time.Since(frameStart); elapsed < framePeriod {
			time.Sleep(framePeriod - elapsed)
		}
	}
}
