package renderer

import (
	"math"
	"slices"

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

// KrakenRenderer shades every tentacle disc solid in the color of the last disc
// covering a cell, rims the rest in the first disc reaching it, and voids the body.
// A tick moves the Kraken and three frames show it, so the cells are shaded once
// per change and drawn again as they are. A dead Kraken's last pose burns away
// under the living.
type KrakenRenderer struct {
	gameCtx *engine.GameContext
	palette bool

	shaded  krakenShade
	samples []component.KrakenSample // the pose's own, when the system's are of another
	disc    []int32                  // per visible cell, one past the last sample whose disc covers it
	rim     []int32                  // per visible cell, the key of the first sample whose rim reaches it
	cells   []terminal.Cell
	spans   [][2]int      // per visible row, the columns any rim reached
	colors  []krakenColor // per sample index, resting then attacking
	drawn   []krakenCell
}

// krakenShade is what the shaded cells were computed from: a pose, and for a dead
// Kraken how far it has burnt
type krakenShade struct {
	entity                 core.Entity
	kraken                 component.KrakenComponent
	x, y                   float64
	dead                   bool
	burn                   float64
	minX, minY, maxX, maxY int
}

// cut is the step past which a dead Kraken's legs have burnt away, running from tip
// to root and on into the void; no step of the living is past it
func (s *krakenShade) cut() float64 {
	if !s.dead {
		return math.Inf(1)
	}
	return 1 - s.burn/visual.KrakenBurnLegs
}

// krakenColor is a sample's color, and its console entry in 256 colors, once a cell
// of this shade needed it
type krakenColor struct {
	set   bool
	color color.RGB
	entry uint8
}

// krakenCell is a shaded cell's index into the visible cells and its map position
type krakenCell struct {
	index, x, y int32
}

func NewKrakenRenderer(ctx *engine.GameContext) *KrakenRenderer {
	return &KrakenRenderer{gameCtx: ctx, palette: ctx.World.Resources.Config.ColorMode == terminal.ColorMode256}
}

func (r *KrakenRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	if maxX < minX || maxY < minY {
		return
	}
	buf.SetWriteMask(visual.MaskComposite)
	world := r.gameCtx.World
	burns := world.Resources.Transient.KrakenBurns()
	for i := range burns {
		burn := &burns[i]
		r.draw(ctx, buf, krakenShade{burn.Entity, burn.Kraken, burn.X, burn.Y, true, burn.Burn, minX, minY, maxX, maxY})
	}
	for _, e := range world.Components.Kraken.Entities() {
		k, _ := world.Components.Kraken.GetPtr(e)
		if motion, ok := world.Components.Kinetic.GetPtr(e); ok {
			r.draw(ctx, buf, krakenShade{e, *k, motion.PreciseX, motion.PreciseY, false, 0, minX, minY, maxX, maxY})
		}
	}
}

// draw shades one Kraken, unless it is the one last shaded, and draws its cells
func (r *KrakenRenderer) draw(ctx render.RenderContext, buf *render.RenderBuffer, shade krakenShade) {
	minX, minY := shade.minX, shade.minY
	if shade != r.shaded {
		r.shaded = shade
		// The system sampled a living pose during the tick, unless something moved it since
		var samples []component.KrakenSample
		sampled := false
		if !shade.dead {
			samples, sampled = r.gameCtx.World.Resources.KrakenShape.Samples(shade.entity, &shade.kraken, shade.x, shade.y)
		}
		if !sampled {
			r.samples = shade.kraken.AppendTentacleSamples(r.samples[:0], shade.x, shade.y, shade.burn)
			samples = r.samples
		}
		r.shade(&r.shaded, samples)
	}
	// The visible bounds show as one rectangle, so when both corners show every cell
	// shifts onto the screen by the same offset
	sx0, sy0, first := ctx.MapToScreen(minX, minY)
	_, _, last := ctx.MapToScreen(shade.maxX, shade.maxY)
	for i := range r.drawn {
		c := &r.drawn[i]
		x, y := int(c.x), int(c.y)
		sx, sy, visible := x-minX+sx0, y-minY+sy0, first && last
		if !visible {
			if sx, sy, visible = ctx.MapToScreen(x, y); !visible {
				continue
			}
		}
		cell := &r.cells[c.index]
		if r.palette {
			buf.SetBg256(sx, sy, cell.Bg.R)
			buf.SetFgOnly(sx, sy, cell.Rune, cell.Fg, terminal.AttrFg256)
		} else {
			buf.SetWithBg(sx, sy, cell.Rune, cell.Fg, cell.Bg)
		}
	}
}

// shade computes the cells of one Kraken over the visible map bounds, dropping the
// samples a burn has taken from the slice it is given
func (r *KrakenRenderer) shade(of *krakenShade, samples []component.KrakenSample) {
	k, x, y := &of.kraken, of.x, of.y
	minX, minY, maxX, maxY := of.minX, of.minY, of.maxX, of.maxY
	r.colors = slices.Grow(r.colors[:0], 2*len(samples))[:2*len(samples)]
	clear(r.colors)
	// What a burn has taken is gone before the stamping
	cut := of.cut()
	if cut < 1 {
		kept := samples[:0]
		for i := range samples {
			if samples[i].Step <= cut {
				kept = append(kept, samples[i])
			}
		}
		samples = kept
	}
	r.stamp(samples, minX, minY, maxX, maxY)

	// In 256 colors a cell holds console entries, as its color's red channel
	width, keys := maxX-minX+1, int32(len(samples))
	disc, rim, colors, cells, drawn := r.disc, r.rim, r.colors, r.cells, r.drawn
	background := visual.RgbBackground
	if r.palette {
		background = color.RGB{R: visual.ConBlack}
	}
	for cy, span := range r.spans {
		row := cy*width - minX
		for cx := span[0]; cx <= span[1]; cx++ {
			i := row + cx
			owner, solid := disc[i], true
			if owner == 0 {
				owner, solid = keys+1-rim[i], false
			}
			if owner > keys {
				continue
			}
			s := &samples[owner-1]
			slot := 2 * s.Index
			if s.Attacking {
				slot++
			}
			if !colors[slot].set {
				r.mix(k, s, slot, cut)
			}
			c := colors[slot].color
			if r.palette {
				c = color.RGB{R: colors[slot].entry}
			}
			if solid {
				cells[i] = terminal.Cell{Rune: ' ', Fg: c, Bg: c}
			} else {
				cells[i] = terminal.Cell{Rune: visual.KrakenRimRune, Fg: c, Bg: background}
			}
			drawn = append(drawn, krakenCell{index: int32(i), x: int32(cx), y: int32(minY + cy)})
		}
	}
	r.drawn = drawn

	// The sandbox's default void profile fades smoothly through the leg roots. The
	// legs' burn glows on into it as it reaches their roots, and it closes at the last.
	ember, closing := 1-cut/visual.KrakenBurnEmber, 1.0
	if of.dead {
		closing = min(1, (1-of.burn)/(1-visual.KrakenBurnLegs))
	}
	void, body := color.Lerp(visual.RgbKrakenVoid, visual.RgbKrakenAttack, ember), color.RGB{R: visual.Kraken256Leg}
	if ember >= visual.Effect256Threshold {
		body.R = visual.Kraken256Heat
	}
	rx, ry := parameter.KrakenBodyRadius*parameter.KrakenRowSquash, parameter.KrakenBodyRadius
	for cy := max(minY, int(math.Ceil(y-ry-vmath.CellCenter))); cy <= min(maxY, int(y+ry-vmath.CellCenter)); cy++ {
		for cx := max(minX, int(math.Ceil(x-rx-vmath.CellCenter))); cx <= min(maxX, int(x+rx-vmath.CellCenter)); cx++ {
			nx, ny := (float64(cx)+vmath.CellCenter-x)/rx, (float64(cy)+vmath.CellCenter-y)/ry
			distance := math.Sqrt(nx*nx + ny*ny)
			if distance > 1 {
				continue
			}
			i := (cy-minY)*width + cx - minX
			cell := &r.cells[i]
			if cell.Rune == 0 {
				*cell = terminal.Cell{Rune: ' ', Bg: background}
				r.drawn = append(r.drawn, krakenCell{index: int32(i), x: int32(cx), y: int32(cy)})
			}
			alpha := 1 - distance
			alpha *= alpha * (3 - 2*alpha) * closing
			if r.palette {
				// The console cannot blend the void, so the body is solid in the legs' color
				if alpha >= visual.Effect256Threshold {
					*cell = terminal.Cell{Rune: ' ', Fg: body, Bg: body}
				}
				continue
			}
			cell.Bg = color.Lerp(cell.Bg, void, alpha)
			if alpha > visual.KrakenVoidOpaque {
				cell.Rune = ' '
			}
		}
	}
}

// stamp clears the cells last shaded and marks each visible cell with the disc and
// the rim that own it. It knows nothing of a burn: a value of one held across its
// loop cost the living a twentieth of their shading.
func (r *KrakenRenderer) stamp(samples []component.KrakenSample, minX, minY, maxX, maxY int) {
	width, height := maxX-minX+1, maxY-minY+1
	n := width * height
	for _, c := range r.drawn {
		r.disc[c.index], r.rim[c.index], r.cells[c.index] = 0, 0, terminal.Cell{}
	}
	r.drawn = r.drawn[:0]
	if cap(r.cells) < n {
		r.disc, r.rim, r.cells = make([]int32, n), make([]int32, n), make([]terminal.Cell, n)
	}
	r.disc, r.rim, r.cells = r.disc[:n], r.rim[:n], r.cells[:n]
	r.spans = slices.Grow(r.spans[:0], height)[:height]
	for i := range r.spans {
		r.spans[i] = [2]int{maxX + 1, minX - 1}
	}

	// The last disc covering a cell owns it, so a later sample takes it over. The first
	// rim reaching it owns it, so rims key samples counting down from the first and a
	// cell keeps the greatest key; a rim spans its own disc too, as a covered cell shows
	// none. A squared distance outside the rounding margin of both thresholds decides by
	// comparison alone; within the band the distance itself decides, as the definition has it.
	disc, rim := r.disc, r.rim
	keys := int32(len(samples))
	for i := range samples {
		s := &samples[i]
		sx, radius := s.X, s.Radius
		id, key := int32(i+1), keys-int32(i)
		rimRadius := s.Radius + visual.KrakenRimReach
		discT, rimT := s.Radius*s.Radius, rimRadius*rimRadius
		discIn, rimIn := discT-vmath.RoundingMargin, rimT-vmath.RoundingMargin
		reach := rimRadius + vmath.RoundingMargin
		band := 2 * vmath.RoundingMargin * (rimT - discT + 2*vmath.RoundingMargin)
		x0, x1 := max(minX, int(math.Ceil(s.X-reach-vmath.CellCenter))), min(maxX, int(math.Floor(s.X+reach-vmath.CellCenter)))
		y0, y1 := max(minY, int(math.Ceil(s.Y-reach/parameter.KrakenRowSquash-vmath.CellCenter))), min(maxY, int(math.Floor(s.Y+reach/parameter.KrakenRowSquash-vmath.CellCenter)))
		if x0 > x1 {
			continue
		}
		for cy := y0; cy <= y1; cy++ {
			span := &r.spans[cy-minY]
			span[0], span[1] = min(span[0], x0), max(span[1], x1)
			dy, row := (float64(cy)+vmath.CellCenter-s.Y)*parameter.KrakenRowSquash, (cy-minY)*width-minX
			discRow, rimRow := disc[row+x0:row+x1+1], rim[row+x0:row+x1+1]
			rimRow = rimRow[:len(discRow)]
			column := float64(x0) + vmath.CellCenter
			for c := range discRow {
				dx := column - sx
				column++
				d2 := dx*dx + dy*dy
				owner, first := discRow[c], int32(0)
				if d2 <= discIn {
					owner = id
				}
				if d2 < rimIn {
					first = key
				}
				if math.Abs((d2-discT)*(d2-rimT)) <= band {
					distance := math.Sqrt(d2)
					if distance <= radius {
						owner = id
					}
					if distance-radius < visual.KrakenRimReach {
						first = key
					}
				}
				discRow[c], rimRow[c] = owner, max(rimRow[c], first)
			}
		}
	}
}

// mix stores a sample's color at its slot: its gradient from root to tip, heated
// toward the attack color by a charge, by its own leg's lash and by the burn behind
// the cut. Legs that rest, and legs that attack, share a length, so they share the
// color at each index.
func (r *KrakenRenderer) mix(k *component.KrakenComponent, s *component.KrakenSample, slot int, cut float64) {
	heat := s.Step * s.Step * k.MoveBlend * visual.KrakenMoveHeat
	if s.Attacking {
		heat = max(heat, s.Step*s.Step*k.AttackT)
	}
	if glow := 1 - (cut-s.Step)/visual.KrakenBurnEmber; glow > heat {
		heat = glow
	}
	entry := visual.Kraken256Leg
	if heat >= visual.Effect256Threshold {
		entry = visual.Kraken256Heat
	}
	r.colors[slot] = krakenColor{true, color.Lerp(color.Lerp(visual.RgbKrakenLegBase, visual.RgbKrakenLegTip, s.Step), visual.RgbKrakenAttack, heat), entry}
}
