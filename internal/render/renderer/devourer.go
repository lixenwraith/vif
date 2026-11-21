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
// converts an image. A frame of the body, the tick ascimage's viewer animates on,
// is painted a third a render frame ahead of its turn and drawn until the next; a
// killed Devourer draws back to the edges, fading, under the living.
type DevourerRenderer struct {
	gameCtx *engine.GameContext
	palette bool

	shown, ahead devourerPicture          // what is drawn, and what is being painted
	shape        *component.DevourerShape // the body being painted: the world's, or own
	own          component.DevourerShape
	field        devourerField
	img          []devourerSample // ahead's samples over its bounds, two a cell each way
	depth        []float32        // the depths of one run of samples
	wave         []float64        // Depths' scratch
	near         []gutNear        // per sample, the nearest point of the gut being stamped
	stamp        uint32
	touched      []int32
	rows         []devourerRow // per row of ahead, where its painted cells may lie
	ramp         [256]color.RGB
	wet          [devourerWetSteps]devourerWet
	tube         [devourerTubeSteps][devourerTubeSteps]color.RGB // a gut's shade, across it by along its segment
}

// devourerPicture is the cells fitted from a shade, in runs, and how many steps
// of its painting are done: a row of each body's flesh, its guts, a row fitted
type devourerPicture struct {
	shade   devourerShade
	painted int
	cells   []terminal.Cell
	runs    []devourerRun
}

// The lip and the gloss are tabled over the depth the lip reaches, and a gut's
// tube over its radius and its segment's swell. A gut's centreline is stamped in
// chords that leave it by devourerGutSlack column units at most, within the bounds.
const (
	devourerWetReach    = 3 * visual.DevourerLipWidth
	devourerWetSteps    = 256
	devourerTubeSteps   = 64
	devourerGutSlack    = 0.05
	devourerGutChordMin = 2.0
	devourerGutChordMax = 8.0
)

// devourerRow is a visible row's cells no flesh was laid on, open up to shut, and
// the cells a gut's stamp reached, from through to; the rest may all be painted
type devourerRow struct{ open, shut, from, to int32 }

// devourerWet is the lip's light and the gloss's fade at a depth
type devourerWet struct{ lip, gloss float32 }

// devourerShade is what a picture's cells are fitted from: every body painted, the
// living one last, over the visible bounds
type devourerShade struct {
	sources                [1 + parameter.DevourerRecedeCap]devourerSource
	count                  int
	living                 bool
	minX, minY, maxX, maxY int
}

// after is the shade a frame of the body on, where one living Devourer alone is
// painted: its walls and its guts move as its frame says
func (s *devourerShade) after() (devourerShade, bool) {
	next := *s
	src := &next.sources[0]
	src.frame++
	src.grown = component.DevourerGrown(src.frame)
	return next, s.living && s.count == 1
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

// devourerSample is a sample's color and what painted it, none for an open one:
// markFlesh, and markGut shifted up by its slot for each gut over it
type devourerSample struct {
	c    color.RGB
	mark uint8
}

const (
	markFlesh uint8 = 1 << iota
	markGut
)

// gutNear is a sample's nearest centreline point of the gut stamped as stamp: its
// squared distance and how far along the gut
type gutNear struct {
	d2, u float32
	stamp uint32
}

// gutFlash is a gut's hit flash this frame: its color, at the map cell it lit
type gutFlash struct {
	tint color.RGB
	x, y int
}

// devourerRun is n fitted cells along a row from the map cell x, y, first in
// cells at at, alike in what they are: whole, or with a sample left open, which
// gives way to what is drawn under it, and under the guts whose slots' bits guts holds
type devourerRun struct {
	x, y, at, n int32
	whole       bool
	guts        uint8
}

// devourerField is the texture at each sample of a width by height map, each
// worked out as the flesh first covers it
type devourerField struct {
	seed          uint64
	width, height int
	tex           []devourerTexel
}

// devourerTexel is a sample's texture: how its warp bends the folds, its light
// before them, its grain's share of the gloss and its vein's tint of 256
type devourerTexel struct {
	fold, base, gloss float32
	vein              uint16
	done              bool
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
	for i := range r.wet {
		d := (float64(i) + 0.5) * devourerWetReach / devourerWetSteps
		lip := d / visual.DevourerLipWidth
		r.wet[i].lip = float32(0.35 * math.Exp(-lip*lip))
		// Gloss past 0.55 needs exp(-d/5) past it, within 3 of the wall
		if d < 3 {
			r.wet[i].gloss = float32(math.Exp(-d / 5))
		}
	}
	for i := range r.tube {
		across := (float64(i) + 0.5) / devourerTubeSteps
		lit := math.Sqrt(1 - across*across)
		for j := range r.tube[i] {
			seg := float64(j) / (devourerTubeSteps - 1)
			c := color.Lerp(visual.RgbDevourerGutFlank, visual.RgbDevourerGut, lit*(0.65+0.35*seg))
			if lit > 0.9 && seg > 0.55 {
				c = color.Lerp(c, visual.RgbDevourerGloss, 0.55)
			}
			if seg < 0.16 {
				c = color.Lerp(c, visual.RgbDevourerGutCrease, 0.55)
			}
			r.tube[i][j] = c
		}
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
			shade.count, shade.living = shade.count+1, true
		}
	}
	// The frame painted ahead is this one unless something else changed, and then
	// this render paints it whole and nothing ahead
	missed := false
	if shade != r.shown.shade {
		if missed = shade != r.ahead.shade; missed {
			r.begin(shade)
		}
		r.paint(math.MaxInt, missed)
		r.shown, r.ahead = r.ahead, r.shown
	}
	if next, ok := shade.after(); ok && !missed {
		if next != r.ahead.shade {
			r.begin(next)
		}
		r.paint((2*(maxY-minY+1)+3)/3, false)
	}

	// The last source is the living Devourer, whose guts a hit lights
	var flashes [parameter.DevourerGutCap]gutFlash
	lit := uint8(0)
	for slot, gut := range shade.sources[max(shade.count-1, 0)].guts {
		combat, ok := world.Components.Combat.GetPtr(gut)
		if !ok || combat.RemainingHitFlash <= 0 {
			continue
		}
		at, ok := world.Positions.GetPosition(combat.HitEntity)
		if !ok {
			at, _ = world.Positions.GetPosition(gut)
		}
		flashes[slot] = gutFlash{tint: hitFlashColor(combat.RemainingHitFlash), x: at.X, y: at.Y}
		lit |= 1 << slot
	}

	buf.SetWriteMask(visual.MaskComposite)
	// The visible bounds show as one rectangle, so when both corners show every cell
	// shifts onto the screen by the same offset
	sx0, sy0, first := ctx.MapToScreen(minX, minY)
	_, _, last := ctx.MapToScreen(maxX, maxY)
	for i := range r.shown.runs {
		run := &r.shown.runs[i]
		cells := r.shown.cells[run.at : run.at+run.n]
		x, y := int(run.x), int(run.y)
		if first && last && run.whole && !r.palette && run.guts&lit == 0 {
			buf.SetRunWithBg(x-minX+sx0, y-minY+sy0, cells)
			continue
		}
		for k := range cells {
			sx, sy, visible := x+k-minX+sx0, y-minY+sy0, first && last
			if !visible {
				if sx, sy, visible = ctx.MapToScreen(x+k, y); !visible {
					continue
				}
			}
			// A glyph standing at the wall's edge stays readable
			if !run.whole {
				if under := buf.CellAt(sx, sy).Rune; under != 0 && under != ' ' {
					continue
				}
			}
			c := cells[k]
			for slot := range flashes {
				if run.guts&lit&(1<<slot) == 0 {
					continue
				}
				// A console cannot blend the flash, so it lights the core alone
				flash := &flashes[slot]
				if by := flash.at(x+k, y); !r.palette {
					c.Fg, c.Bg = color.Lerp(c.Fg, flash.tint, by), color.Lerp(c.Bg, flash.tint, by)
				} else if by >= visual.DevourerGutHitFlash {
					c.Rune, c.Fg.R = visual.Devourer256HitRune, visual.Devourer256Hit
				}
			}
			if r.palette {
				buf.SetBg256(sx, sy, c.Bg.R)
				buf.SetFgOnly(sx, sy, c.Rune, c.Fg, terminal.AttrFg256)
			} else {
				buf.SetWithBg(sx, sy, c.Rune, c.Fg, c.Bg)
			}
		}
	}
}

// at is how far the flash lights the map cell x, y toward its color: whole at
// its core, fading out to its reach, a row two column units
func (f *gutFlash) at(x, y int) float64 {
	dist := math.Hypot(float64(x-f.x), 2*float64(y-f.y))
	fade := (visual.DevourerGutHitReach - dist) / (visual.DevourerGutHitReach - visual.DevourerGutHitCore)
	return visual.DevourerGutHitFlash * min(max(fade, 0), 1)
}

// begin readies ahead to be painted from a shade
func (r *DevourerRenderer) begin(shade devourerShade) {
	w, h := shade.maxX-shade.minX+1, shade.maxY-shade.minY+1
	n := 4 * w * h
	r.img = slices.Grow(r.img[:0], n)[:n]
	clear(r.img)
	if len(r.near) < n {
		r.near, r.stamp = make([]gutNear, n), 0
	}
	r.depth = slices.Grow(r.depth[:0], 2*w)[:2*w]
	r.rows = slices.Grow(r.rows[:0], h)[:h]
	for i := range r.rows {
		r.rows[i] = devourerRow{open: 0, shut: int32(w), from: int32(w), to: -1}
	}
	p := &r.ahead
	p.shade, p.painted, p.cells, p.runs = shade, 0, slices.Grow(p.cells[:0], h*w), p.runs[:0]
}

// paint takes ahead's painting n steps on. A body's shape is the world's where
// borrow says the painting ends in this call and its system built that frame,
// since a later tick builds the world's anew.
func (r *DevourerRenderer) paint(n int, borrow bool) {
	p := &r.ahead
	shade := &p.shade
	h := shade.maxY - shade.minY + 1
	body := h + 1
	for total := shade.count*body + h; n > 0 && p.painted < total; n, p.painted = n-1, p.painted+1 {
		if p.painted >= shade.count*body {
			r.fitRow(p, p.painted-shade.count*body)
			continue
		}
		src, row := &shade.sources[p.painted/body], p.painted%body
		if row == 0 {
			r.shape = r.gameCtx.World.Resources.DevourerShape
			if !borrow || !r.shape.Built(src.seed, src.frame, src.grown, src.width, src.height) {
				r.own.Build(src.seed, src.frame, src.grown, src.width, src.height)
				r.shape = &r.own
			}
			r.field.reset(src.seed, src.width, src.height)
		}
		if row < h {
			r.fleshRow(shade, src, row)
			continue
		}
		for slot, gut := range src.guts {
			if g, ok := r.gameCtx.World.Components.DevourerGut.GetPtr(gut); ok {
				r.paintGut(shade, src, g, slot)
			}
		}
	}
}

// fleshRow lays the samples a body covers on the visible row cy, passing the cells
// its wall cannot reach
func (r *DevourerRenderer) fleshRow(shade *devourerShade, src *devourerSource, cy int) {
	minX, minY, w := shade.minX, shade.minY, shade.maxX-shade.minX+1
	// A body covers no cell past its own map
	cols := min(w, src.width-minX)
	if cols <= 0 || cy >= src.height-minY {
		return
	}
	// A sample lies within 0.6 column units of its cell's centre
	open, shut := cols, cols
	if lo, hi := r.shape.Span(minY+cy, 0.6); lo <= hi {
		open, shut = min(max(lo-minX, 0), cols), min(max(hi+1-minX, 0), cols)
	}
	if row := &r.rows[cy]; cols == w {
		row.open, row.shut = max(row.open, int32(open)), min(row.shut, int32(shut))
	} else {
		row.open, row.shut = int32(w), int32(w)
	}
	for sy := 2 * cy; sy < 2*cy+2; sy++ {
		r.fleshRun(src, 2*minX, 2*minY+sy, sy*2*w, 2*open)
		r.fleshRun(src, 2*(minX+shut), 2*minY+sy, sy*2*w+2*shut, 2*(cols-shut))
	}
}

// fleshRun lays n samples of the map's sample row fy from its sample fx on, the
// image's from at: folds along the wall drifting outward, a wet lip at it, a grain,
// veins, a beat, and gloss on the folds' crests by the wall. Every input is the
// plane's or the depth's, so nothing seams.
func (r *DevourerRenderer) fleshRun(src *devourerSource, fx, fy, at, n int) {
	if n <= 0 {
		return
	}
	depth, img := r.depth[:n], r.img[at:at+n]
	r.wave = r.shape.Depths(depth, r.wave, (float64(fx)+0.5)/2, 0.5, float64(fy)+0.5)
	tex := r.field.tex[fy*2*r.field.width+fx:]
	tex = tex[:n]
	t := float64(src.frame) * parameter.DevourerFrame.Seconds()
	fold, beat := -t*visual.DevourerFoldSpeed, 2*math.Pi*visual.DevourerPulseRate*t
	for k, deep := range depth {
		if deep <= 0 {
			continue
		}
		d, x := float64(deep), &tex[k]
		if !x.done {
			r.field.work(x, fx+k, fy)
		}
		ridge := 0.5 + 0.5*vmath.Sin(d*visual.DevourerFoldWave+fold+float64(x.fold))
		ridge *= ridge
		v, gloss := float64(x.base)+0.34*ridge, 0.0
		if d < devourerWetReach {
			wet := &r.wet[int(d*(devourerWetSteps/devourerWetReach))]
			v += float64(wet.lip)
			gloss = ridge * float64(wet.gloss) * float64(x.gloss)
		}
		if d > visual.DevourerDeepFrom+visual.DevourerDeepSpan {
			v -= 0.18
		} else if d > visual.DevourerDeepFrom {
			v -= (d - visual.DevourerDeepFrom) * (0.18 / visual.DevourerDeepSpan)
		}
		v *= 1 - visual.DevourerPulse + visual.DevourerPulse*vmath.Sin(beat-d*0.15)
		c := r.ramp[255]
		if v < 1 {
			c = r.ramp[uint8(max(v, 0)*255)]
		}
		if x.vein != 0 {
			c = color.LerpFixed(c, visual.RgbDevourerVein, int64(x.vein), 8)
		}
		if gloss > 0.55 {
			c = color.Lerp(c, visual.RgbDevourerGloss, min((gloss-0.55)*2.2, 1))
		}
		s := &img[k]
		if src.alpha < 1 {
			c = color.Lerp(r.under(s), c, src.alpha)
		}
		s.c, s.mark = c, s.mark|markFlesh
	}
}

// paintGut lays the gut in a slot over the image: each sample takes the nearest
// point of its centreline, stamped chord by chord, and shades as a tube there,
// segments swelling and crawling along it, tapering into the walls and edged dark
func (r *DevourerRenderer) paintGut(shade *devourerShade, src *devourerSource, g *component.DevourerGutComponent, slot int) {
	w, h := 2*(shade.maxX-shade.minX+1), 2*(shade.maxY-shade.minY+1)
	// The sample sx, sy lies at ox+sx/2, oy+sy
	ox, oy := float64(shade.minX)+0.25, 2*float64(shade.minY)+0.5
	x0, y0, x1, y1 := r.shape.GutEnds(g.From, g.To)
	age := float64(src.frame-g.Formed) * parameter.DevourerFrame.Seconds()
	length := math.Hypot(x1-x0, y1-y0)
	girth := parameter.DevourerGutRadius*visual.DevourerGutBulk*1.08 + 0.6
	// Its sag and its writhe bend it by no more than this
	bend := min(age, 1) * math.Pi * math.Pi * (parameter.DevourerGutSag + 9*parameter.DevourerGutWrithe) / max(length*length, 1)
	chord := min(max(math.Sqrt(8*devourerGutSlack/max(bend, 1e-9)), devourerGutChordMin), devourerGutChordMax)
	chords := max(int(math.Ceil(length/chord)), 1)
	r.stamp++
	r.touched = r.touched[:0]
	line := component.NewGutLine(x0, y0, x1, y1, age, g.Phase)
	ax, ay := line.Point(0)
	for i := range chords {
		bx, by := line.Point(float64(i+1) / float64(chords))
		dx, dy := bx-ax, by-ay
		span := dx*dx + dy*dy
		if span == 0 {
			continue
		}
		// A chord stamps the samples beside it, and past its ends by what a bend
		// opens between it and the next; the gut's own ends are rounded
		reach, before, after := girth*math.Sqrt(span), girth*bend*1.1+1e-6, girth*bend*1.1+1e-6
		if i == 0 {
			before = girth / math.Sqrt(span)
		}
		if i == chords-1 {
			after = girth / math.Sqrt(span)
		}
		sy0, sy1 := max(0, int(math.Ceil(min(ay, by)-girth-oy))), min(h-1, int(math.Floor(max(ay, by)+girth-oy)))
		for sy := sy0; sy <= sy1; sy++ {
			py := oy + float64(sy) - ay
			lo, hi := -girth-math.Abs(dx), girth+math.Abs(dx)
			if dx != 0 {
				a, b := (-before*span-py*dy)/dx, ((1+after)*span-py*dy)/dx
				lo, hi = max(lo, min(a, b)), min(hi, max(a, b))
			} else if along := py * dy / span; along < -before || along > 1+after {
				continue
			}
			if dy != 0 {
				a, b := (py*dx-reach)/dy, (py*dx+reach)/dy
				lo, hi = max(lo, min(a, b)), min(hi, max(a, b))
			}
			sx0, sx1 := max(0, int(math.Ceil(2*(lo+ax-ox)))), min(w-1, int(math.Floor(2*(hi+ax-ox))))
			if sx0 > sx1 {
				continue
			}
			row := &r.rows[sy/2]
			row.from, row.to = min(row.from, int32(sx0/2)), max(row.to, int32(sx1/2))
			for sx := sx0; sx <= sx1; sx++ {
				px := ox + float64(sx)/2 - ax
				along := (px*dx + py*dy) / span
				if along < 0 {
					along = 0
				} else if along > 1 {
					along = 1
				}
				ex, ey := px-along*dx, py-along*dy
				d2 := float32(ex*ex + ey*ey)
				near := &r.near[sy*w+sx]
				if near.stamp != r.stamp {
					r.touched = append(r.touched, int32(sy*w+sx))
				} else if d2 >= near.d2 {
					continue
				}
				*near = gutNear{d2: d2, u: float32((float64(i) + along) / float64(chords)), stamp: r.stamp}
			}
		}
		ax, ay = bx, by
	}
	t := float64(src.frame) * parameter.DevourerFrame.Seconds()
	swell, crawl := length/visual.DevourerGutSegment*2*math.Pi, t*visual.DevourerGutCrawl
	mark := markGut << slot
	for _, k := range r.touched {
		near := &r.near[k]
		u, dist := float64(near.u), math.Sqrt(float64(near.d2))
		seg := 0.5 + 0.5*vmath.Cos(u*swell-crawl)
		taper := 1.0
		if end := min(u, 1-u) * 6; end < 1 {
			taper = 0.55 + 0.45*math.Sqrt(end)
		}
		rad := parameter.DevourerGutRadius * visual.DevourerGutBulk * (0.7 + 0.38*seg) * taper
		if dist > rad+0.6 {
			continue
		}
		s := &r.img[k]
		base := r.under(s)
		c := color.LerpFixed(base, visual.RgbDevourerGutRim, 179, 8)
		if dist <= rad {
			c = r.tube[min(int(dist/rad*devourerTubeSteps), devourerTubeSteps-1)][int(seg*(devourerTubeSteps-1)+0.5)]
		}
		if src.alpha < 1 {
			c = color.Lerp(base, c, src.alpha)
		}
		s.c, s.mark = c, s.mark|mark
	}
}

// under is what a sample shows before a body paints it
func (r *DevourerRenderer) under(s *devourerSample) color.RGB {
	if s.mark != 0 {
		return s.c
	}
	return visual.RgbBackground
}

// fitRow gives each cell of the visible row cy that the image covers its cell: a
// quadrant glyph and two colors fitted to its four samples, the open ones the
// background's; in 256 colors the console tier nearest their mean, for a cell half
// covered or more
func (r *DevourerRenderer) fitRow(p *devourerPicture, cy int) {
	row, w := &r.rows[cy], p.shade.maxX-p.shade.minX+1
	if row.open >= row.shut {
		r.fitRun(p, cy, 0, w)
		return
	}
	// No flesh lies on the open run: only a gut's stamp paints there
	r.fitRun(p, cy, 0, int(row.open))
	r.fitRun(p, cy, int(max(row.open, row.from)), int(min(row.shut, row.to+1)))
	r.fitRun(p, cy, int(row.shut), w)
}

// fitRun fits the cells from through to-1 of the visible row cy that hold a
// painted sample, each joining the run before it where it stands beside it alike
func (r *DevourerRenderer) fitRun(p *devourerPicture, cy, from, to int) {
	shade := &p.shade
	w := shade.maxX - shade.minX + 1
	top := r.img[2*cy*2*w : (2*cy+1)*2*w]
	low := r.img[(2*cy+1)*2*w : (2*cy+2)*2*w]
	var run *devourerRun
	for cx := from; cx < to; cx++ {
		quad := [4]devourerSample{top[2*cx], top[2*cx+1], low[2*cx], low[2*cx+1]}
		px := [4]color.RGB{quad[0].c, quad[1].c, quad[2].c, quad[3].c}
		whole := quad[0].mark != 0 && quad[1].mark != 0 && quad[2].mark != 0 && quad[3].mark != 0
		if !whole {
			painted := 0
			for q := range quad {
				if quad[q].mark == 0 {
					px[q] = visual.RgbBackground
				} else {
					painted++
				}
			}
			if painted == 0 || r.palette && painted < 2 {
				run = nil
				continue
			}
		}
		guts := (quad[0].mark | quad[1].mark | quad[2].mark | quad[3].mark) / markGut
		if run == nil || run.whole != whole || run.guts != guts {
			p.runs = append(p.runs, devourerRun{
				x: int32(shade.minX + cx), y: int32(shade.minY + cy), at: int32(len(p.cells)), whole: whole, guts: guts,
			})
			run = &p.runs[len(p.runs)-1]
		}
		run.n++
		if r.palette {
			p.cells = append(p.cells, devourerTier(px))
		} else {
			rn, fg, bg := ascimage.FitQuadrant(px)
			p.cells = append(p.cells, terminal.Cell{Rune: rn, Fg: fg, Bg: bg})
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
	if seed == f.seed && width == f.width && height == f.height && f.tex != nil {
		return
	}
	f.seed, f.width, f.height = seed, width, height
	n := 4 * width * height
	f.tex = slices.Grow(f.tex[:0], n)[:n]
	clear(f.tex)
}

// work fills the texture of the map's sample fx, fy, two a cell each way
func (f *devourerField) work(x *devourerTexel, fx, fy int) {
	px, py := (float64(fx)+0.5)/2, float64(fy)+0.5
	warp := component.DevourerField(f.seed, px*visual.DevourerWarpScale, py*visual.DevourerWarpScale, 0.5, 3)
	grain := component.DevourerField(f.seed+16, px*visual.DevourerGrainScale, py*visual.DevourerGrainScale, 0.5, 4)
	ridge := component.DevourerField(f.seed+32, px*visual.DevourerVeinScale+0.3*warp, py*visual.DevourerVeinScale, 0.5, 4)
	*x = devourerTexel{
		fold: float32(visual.DevourerFoldWarp * warp), base: float32(0.42 + 0.22*grain),
		gloss: float32(min(max(grain*2+0.3, 0), 1)), done: true,
	}
	if vein := math.Pow(min(max(1-math.Abs(ridge), 0), 1), visual.DevourerVeinSharp); vein > 0.01 {
		x.vein = uint16(min(vein*1.4, 1) * 256)
	}
}
