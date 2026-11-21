package renderer

import (
	"math"
	"slices"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// DevourerRenderer paints a Devourer's flesh and guts as an image of two samples a
// cell each way, and fits each cell a quadrant glyph and two colors as ascimage
// converts an image. It paints once a frame of the body, the tick ascimage's viewer
// animates on, and draws the cells it fitted until the next; a killed Devourer draws
// back to the edges, fading, under the living.
type DevourerRenderer struct {
	gameCtx *engine.GameContext
	palette bool

	shaded  devourerShade
	shape   component.DevourerShape
	field   devourerField
	img     []devourerSample // the samples of the visible bounds, two a cell each way
	near    []gutNear        // per sample, the nearest point of the gut being stamped
	stamp   uint32
	touched []int32
	cells   []devourerCell
	ramp    [256]color.RGB
}

// devourerShade is what the cells were fitted from: every body painted, over the
// visible bounds
type devourerShade struct {
	sources                [1 + parameter.DevourerRecedeCap]devourerSource
	count                  int
	minX, minY, maxX, maxY int
}

// devourerSource is one body painted at its frame, grown in by grown and opaque by
// alpha, with its guts
type devourerSource struct {
	seed          uint64
	frame         int64
	grown, alpha  float64
	width, height int
	guts          [parameter.DevourerGutCap]core.Entity
}

type devourerSample struct {
	c       color.RGB
	painted bool
}

// gutNear is a sample's nearest centreline point of the gut stamped as stamp: its
// squared distance and how far along the gut
type gutNear struct {
	d2, u float32
	stamp uint32
}

// devourerCell is a fitted cell at its map position; one not whole has a sample
// left open, and gives way to what is drawn under it
type devourerCell struct {
	x, y  int32
	cell  terminal.Cell
	whole bool
}

// devourerField is the texture at each sample of a width by height map: its warp,
// grain and vein strength, each worked out as the flesh first covers it
type devourerField struct {
	seed               uint64
	width, height      int
	done               []bool
	warp, grain, veins []float32
}

func NewDevourerRenderer(ctx *engine.GameContext) *DevourerRenderer {
	r := &DevourerRenderer{gameCtx: ctx, palette: ctx.World.Resources.Config.ColorMode == terminal.ColorMode256}
	stops := visual.RgbDevourerFlesh
	for i := range r.ramp {
		v, k := float64(i)/255, 1
		for k < len(stops)-1 && v > stops[k].At {
			k++
		}
		t := (v - stops[k-1].At) / (stops[k].At - stops[k-1].At)
		r.ramp[i] = color.Lerp(stops[k-1].Color, stops[k].Color, min(max(t, 0), 1))
	}
	return r
}

func (r *DevourerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	recedes := world.Resources.Transient.DevourerRecedes()
	if world.Components.Devourer.CountEntities() == 0 && len(recedes) == 0 {
		return
	}
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	if maxX < minX || maxY < minY {
		return
	}
	shade := devourerShade{minX: minX, minY: minY, maxX: maxX, maxY: maxY}
	steps := float64(parameter.DevourerRecede / parameter.DevourerFrame)
	for i := range recedes {
		rc := &recedes[i]
		if shade.count < len(shade.sources) {
			// It draws back a frame at a time, as it stood
			k := math.Floor(rc.Recede*steps) / steps
			shade.sources[shade.count] = devourerSource{seed: rc.Devourer.Seed, frame: rc.Devourer.Frame(),
				grown: rc.Grown * (1 - k), alpha: 1 - 0.85*k, width: rc.Width, height: rc.Height}
			shade.count++
		}
	}
	cfg := world.Resources.Config
	for _, e := range world.Components.Devourer.Entities() {
		if d, ok := world.Components.Devourer.GetPtr(e); ok && shade.count < len(shade.sources) {
			shade.sources[shade.count] = devourerSource{seed: d.Seed, frame: d.Frame(), grown: d.Grown(), alpha: 1,
				width: cfg.MapWidth, height: cfg.MapHeight, guts: d.Guts}
			shade.count++
		}
	}
	if shade != r.shaded {
		r.shaded = shade
		r.paint(&r.shaded)
	}

	buf.SetWriteMask(visual.MaskComposite)
	for i := range r.cells {
		c := &r.cells[i]
		sx, sy, visible := ctx.MapToScreen(int(c.x), int(c.y))
		if !visible {
			continue
		}
		// A glyph standing at the wall's edge stays readable
		if under := buf.CellAt(sx, sy).Rune; !c.whole && under != 0 && under != ' ' {
			continue
		}
		if r.palette {
			buf.SetBg256(sx, sy, c.cell.Bg.R)
			buf.SetFgOnly(sx, sy, c.cell.Rune, c.cell.Fg, terminal.AttrFg256)
		} else {
			buf.SetWithBg(sx, sy, c.cell.Rune, c.cell.Fg, c.cell.Bg)
		}
	}
}

// paint lays every body of a shade into the image and fits its cells
func (r *DevourerRenderer) paint(shade *devourerShade) {
	w, h := shade.maxX-shade.minX+1, shade.maxY-shade.minY+1
	n := 4 * w * h
	r.img = slices.Grow(r.img[:0], n)[:n]
	clear(r.img)
	if len(r.near) < n {
		r.near, r.stamp = make([]gutNear, n), 0
	}
	for i := range shade.count {
		src := &shade.sources[i]
		r.shape.Build(src.seed, src.frame, src.grown, src.width, src.height)
		r.paintFlesh(shade, src)
		for _, gut := range src.guts {
			if g, ok := r.gameCtx.World.Components.DevourerGut.GetPtr(gut); ok {
				r.paintGut(shade, src, g)
			}
		}
	}
	r.fit(shade)
}

// paintFlesh lays the samples a body covers, passing the cells its wall cannot reach
func (r *DevourerRenderer) paintFlesh(shade *devourerShade, src *devourerSource) {
	minX, minY, w := shade.minX, shade.minY, shade.maxX-shade.minX+1
	t := float64(src.frame) * parameter.DevourerFrame.Seconds()
	f := &r.field
	f.reset(src.seed, src.width, src.height)
	for cy := range shade.maxY - minY + 1 {
		// A sample lies within 0.6 column units of its cell's centre
		lo, hi := r.shape.Span(minY+cy, 0.6)
		for cx := 0; cx < w; cx++ {
			if x := minX + cx; x == max(lo, minX) && lo <= hi {
				cx = hi - minX
				continue
			}
			for q := range 4 {
				sx, sy := 2*cx+q%2, 2*cy+q/2
				x, y := float64(minX)+(float64(sx)+0.5)/2, 2*float64(minY)+float64(sy)+0.5
				d := r.shape.Depth(x, y)
				if d <= 0 {
					continue
				}
				s := &r.img[sy*2*w+sx]
				c := r.flesh(f, 2*minX+sx, 2*minY+sy, d, t)
				if src.alpha < 1 {
					c = color.Lerp(r.under(s), c, src.alpha)
				}
				s.c, s.painted = c, true
			}
		}
	}
}

// flesh is the flesh d deep at a sample: folds along the wall drifting outward, a
// wet lip at it, a grain, veins, a beat, and gloss on the folds' crests by the wall.
// Every input is the plane's or the depth's, so nothing seams.
func (r *DevourerRenderer) flesh(f *devourerField, fx, fy int, d, t float64) color.RGB {
	warp, grain, vein := f.at(fx, fy)
	folds := vmath.Sin(d*visual.DevourerFoldWave - t*visual.DevourerFoldSpeed + visual.DevourerFoldWarp*warp)
	ridge := (0.5 + 0.5*folds) * (0.5 + 0.5*folds)
	v := 0.42 + 0.34*ridge + 0.22*grain
	if lip := d / visual.DevourerLipWidth; lip < 3 {
		v += 0.35 * math.Exp(-lip*lip)
	}
	v -= 0.18 * min(max((d-visual.DevourerDeepFrom)/visual.DevourerDeepSpan, 0), 1)
	v *= 1 - visual.DevourerPulse + visual.DevourerPulse*vmath.Sin(2*math.Pi*visual.DevourerPulseRate*t-d*0.15)
	c := r.ramp[uint8(min(max(v, 0), 1)*255)]
	if vein > 0.01 {
		c = color.Lerp(c, visual.RgbDevourerVein, min(vein*1.4, 1))
	}
	// Gloss past 0.55 needs exp(-d/5) past it, within 3 of the wall
	if d < 3 {
		if gloss := ridge * math.Exp(-d/5) * min(max(grain*2+0.3, 0), 1); gloss > 0.55 {
			c = color.Lerp(c, visual.RgbDevourerGloss, min((gloss-0.55)*2.2, 1))
		}
	}
	return c
}

// paintGut lays a gut over the image: each sample takes the nearest point of its
// centreline, stamped along it, and shades as a tube there, segments swelling and
// crawling along it, tapering into the walls and edged dark
func (r *DevourerRenderer) paintGut(shade *devourerShade, src *devourerSource, g *component.DevourerGutComponent) {
	minX, minY := shade.minX, shade.minY
	w, h := 2*(shade.maxX-minX+1), 2*(shade.maxY-minY+1)
	x0, y0, x1, y1 := r.shape.GutEnds(g.From, g.To)
	age := float64(src.frame-g.Formed) * parameter.DevourerFrame.Seconds()
	length := math.Hypot(x1-x0, y1-y0)
	girth := parameter.DevourerGutRadius*visual.DevourerGutBulk*1.08 + 0.6
	r.stamp++
	r.touched = r.touched[:0]
	steps := max(int(length/0.25), 1)
	for i := range steps + 1 {
		u := float64(i) / float64(steps)
		px, py := component.GutPoint(x0, y0, x1, y1, u, age, g.Phase)
		sx0, sx1 := max(0, int(math.Ceil(2*(px-girth-float64(minX))-0.5))), min(w-1, int(2*(px+girth-float64(minX))-0.5))
		sy0, sy1 := max(0, int(math.Ceil(py-girth-2*float64(minY)-0.5))), min(h-1, int(py+girth-2*float64(minY)-0.5))
		for sy := sy0; sy <= sy1; sy++ {
			dy := 2*float64(minY) + float64(sy) + 0.5 - py
			for sx := sx0; sx <= sx1; sx++ {
				dx := float64(minX) + (float64(sx)+0.5)/2 - px
				k, d2 := sy*w+sx, float32(dx*dx+dy*dy)
				near := &r.near[k]
				if near.stamp != r.stamp {
					*near = gutNear{d2: d2, u: float32(u), stamp: r.stamp}
					r.touched = append(r.touched, int32(k))
				} else if d2 < near.d2 {
					near.d2, near.u = d2, float32(u)
				}
			}
		}
	}
	t := float64(src.frame) * parameter.DevourerFrame.Seconds()
	for _, k := range r.touched {
		near := &r.near[k]
		u, dist := float64(near.u), math.Sqrt(float64(near.d2))
		seg := 0.5 + 0.5*math.Cos(u*length/visual.DevourerGutSegment*2*math.Pi-t*visual.DevourerGutCrawl)
		taper := 0.55 + 0.45*math.Sqrt(min(min(u, 1-u)*6, 1))
		rad := parameter.DevourerGutRadius * visual.DevourerGutBulk * (0.7 + 0.38*seg) * taper
		if dist > rad+0.6 {
			continue
		}
		s := &r.img[k]
		base := r.under(s)
		var c color.RGB
		if dist > rad {
			c = color.Lerp(base, visual.RgbDevourerGutRim, 0.7)
		} else {
			across := dist / rad
			lit := math.Sqrt(1 - across*across)
			c = color.Lerp(visual.RgbDevourerGutFlank, visual.RgbDevourerGut, lit*(0.65+0.35*seg))
			if lit > 0.9 && seg > 0.55 {
				c = color.Lerp(c, visual.RgbDevourerGloss, 0.55)
			}
			if seg < 0.16 {
				c = color.Lerp(c, visual.RgbDevourerGutCrease, 0.55)
			}
		}
		if src.alpha < 1 {
			c = color.Lerp(base, c, src.alpha)
		}
		s.c, s.painted = c, true
	}
}

// under is what a sample shows before a body paints it
func (r *DevourerRenderer) under(s *devourerSample) color.RGB {
	if s.painted {
		return s.c
	}
	return visual.RgbBackground
}

// fit gives each cell the image covers its cell: a quadrant glyph and two colors
// fitted to its four samples, the open ones the background's; in 256 colors the
// console tier nearest their mean, for a cell half covered or more
func (r *DevourerRenderer) fit(shade *devourerShade) {
	r.cells = r.cells[:0]
	w := shade.maxX - shade.minX + 1
	for cy := range shade.maxY - shade.minY + 1 {
		for cx := range w {
			var px [4]color.RGB
			painted := 0
			for q := range 4 {
				s := &r.img[(2*cy+q/2)*2*w+2*cx+q%2]
				px[q] = r.under(s)
				if s.painted {
					painted++
				}
			}
			if painted == 0 || r.palette && painted < 2 {
				continue
			}
			cell := devourerCell{x: int32(shade.minX + cx), y: int32(shade.minY + cy), whole: painted == 4}
			if r.palette {
				cell.cell = devourerTier(px)
			} else {
				rn, fg, bg := ascimage.FitQuadrant(px)
				cell.cell = terminal.Cell{Rune: rn, Fg: fg, Bg: bg}
			}
			r.cells = append(r.cells, cell)
		}
	}
}

// devourerTier is the console cell whose tier stands nearest the samples' mean
func devourerTier(px [4]color.RGB) terminal.Cell {
	var sum [3]int
	for _, p := range px {
		sum[0], sum[1], sum[2] = sum[0]+int(p.R), sum[1]+int(p.G), sum[2]+int(p.B)
	}
	best, bestDist := 0, math.MaxInt
	for i, tier := range visual.Devourer256Tiers {
		dr, dg, db := sum[0]/4-int(tier.Color.R), sum[1]/4-int(tier.Color.G), sum[2]/4-int(tier.Color.B)
		if dist := dr*dr + dg*dg + db*db; dist < bestDist {
			best, bestDist = i, dist
		}
	}
	tier := &visual.Devourer256Tiers[best]
	return terminal.Cell{Rune: tier.Rune, Fg: color.RGB{R: tier.Fg}, Bg: color.RGB{R: tier.Bg}}
}

// reset readies the field for a map and a seed, keeping what it worked out for them
func (f *devourerField) reset(seed uint64, width, height int) {
	if seed == f.seed && width == f.width && height == f.height && f.done != nil {
		return
	}
	f.seed, f.width, f.height = seed, width, height
	n := 4 * width * height
	f.done = slices.Grow(f.done[:0], n)[:n]
	clear(f.done)
	f.warp = slices.Grow(f.warp[:0], n)[:n]
	f.grain = slices.Grow(f.grain[:0], n)[:n]
	f.veins = slices.Grow(f.veins[:0], n)[:n]
}

// at is the texture at the map's sample fx, fy, two a cell each way
func (f *devourerField) at(fx, fy int) (warp, grain, vein float64) {
	i := fy*2*f.width + fx
	if !f.done[i] {
		x, y := (float64(fx)+0.5)/2, float64(fy)+0.5
		warp := component.DevourerField(f.seed, x*visual.DevourerWarpScale, y*visual.DevourerWarpScale, 0.5, 3)
		grain := component.DevourerField(f.seed+16, x*visual.DevourerGrainScale, y*visual.DevourerGrainScale, 0.5, 4)
		ridge := component.DevourerField(f.seed+32, x*visual.DevourerVeinScale+0.3*warp, y*visual.DevourerVeinScale, 0.5, 4)
		vein := math.Pow(min(max(1-math.Abs(ridge), 0), 1), visual.DevourerVeinSharp)
		f.warp[i], f.grain[i], f.veins[i], f.done[i] = float32(warp), float32(grain), float32(vein), true
	}
	return float64(f.warp[i]), float64(f.grain[i]), float64(f.veins[i])
}
