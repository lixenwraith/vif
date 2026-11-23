package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// SlideRenderer draws each slide pulse as a swell running out through the frame: a
// thick ring raised off the surface behind the pulse's edge, which lights what lies
// under it by its slopes, bends it as a lens does and glows. It draws in half
// cells, so the ring's edges are round.
type SlideRenderer struct {
	gameCtx *engine.GameContext
	under   []terminal.Cell // the frame as it stood under the swells, which they bend
	width   int
}

func NewSlideRenderer(gameCtx *engine.GameContext) *SlideRenderer {
	return &SlideRenderer{gameCtx: gameCtx}
}

func (r *SlideRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pulses := r.gameCtx.World.Components.Pulse
	if pulses.CountEntities() == 0 {
		return
	}
	held := false
	pulses.Each(func(_ core.Entity, p *component.PulseComponent) bool {
		if p.Style != component.PulseSlide || p.Radius <= 0 {
			return true
		}
		if !held {
			r.hold(buf)
			buf.SetWriteMask(visual.MaskTransient)
			held = true
		}
		r.swell(ctx, buf, p)
		return true
	})
}

// hold keeps the frame as drawn so far, each cell with the background it shows
func (r *SlideRenderer) hold(buf *render.RenderBuffer) {
	bounds := buf.Bounds()
	r.width = bounds.X1 - bounds.X0
	r.under = r.under[:0]
	for y := bounds.Y0; y < bounds.Y1; y++ {
		for x := bounds.X0; x < bounds.X1; x++ {
			cell := buf.CellAt(x, y)
			cell.Bg = buf.BackgroundAt(x, y, visual.RgbBackground)
			r.under = append(r.under, cell)
		}
	}
}

// swell draws the cells between the pulse's edge and SlideBand behind it in half
// cells, and a glyph it bends into a cell as that glyph
func (r *SlideRenderer) swell(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent) {
	front := p.Radius * p.Size()
	startX, endX, startY, endY, ok := effectBounds(ctx, p.X, p.Y, front, ctx.CellAspect.FromCircular(front))
	if !ok {
		return
	}
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			sx, sy, visible := ctx.MapToScreen(x, y)
			if !visible {
				continue
			}
			top := r.film(ctx, p, front, float64(x), float64(y)-0.25)
			bottom := r.film(ctx, p, front, float64(x), float64(y)+0.25)
			if top.height <= 0 && bottom.height <= 0 {
				continue
			}
			upper, lower := top.over(top.under.Bg), bottom.over(bottom.under.Bg)
			if glyph := top.under; glyph.Rune != 0 && glyph.Rune != ' ' {
				buf.SetWithBg(sx, sy, glyph.Rune, top.over(glyph.Fg), color.Lerp(upper, lower, 0.5))
				continue
			}
			buf.SetWithBg(sx, sy, '▀', upper, lower)
		}
	}
}

// slideFilm is the swell over a point of the map: how high it stands there, the
// light its slope takes, its own color, and the held cell it bends into view
type slideFilm struct {
	height, shade, gloss float64
	body                 color.RGB
	under                terminal.Cell
}

// over is a color of the held frame as seen through the film
func (f slideFilm) over(c color.RGB) color.RGB {
	if f.height <= 0 {
		return c
	}
	c = color.Lerp(color.Scale(c, f.shade), f.body, visual.SlideGlow*f.height)
	return color.Add(c, color.White, f.gloss)
}

func (r *SlideRenderer) film(ctx render.RenderContext, p *component.PulseComponent, front, x, y float64) slideFilm {
	dx, dy := x-float64(p.X), ctx.CellAspect.Circular(y-float64(p.Y))
	dist := math.Sqrt(dx*dx + dy*dy)
	across := (front - dist) / visual.SlideBand
	if across <= 0 || across >= 1 || dist == 0 {
		return slideFilm{under: r.at(ctx, x, y)}
	}
	// A raised cosine across the band, flat at both edges, its crest drawn toward
	// the leading one by SlideSkew
	sin, cos := math.Sincos(2 * math.Pi * across * (1 + visual.SlideSkew*(1-across)))
	outX, outY := dx/dist, dy/dist
	slope := visual.SlideRise * math.Pi * sin * (1 + visual.SlideSkew*(1-2*across)) / visual.SlideBand
	nx, ny, nz := slope*outX, slope*outY, 1.0
	norm := 1 / math.Sqrt(nx*nx+ny*ny+1)
	nx, ny, nz = nx*norm, ny*norm, nz*norm

	// The lamp's light on the slope against its light on the flat, and its mirror
	f := slideFilm{height: (1 - cos) / 2}
	lit := max(nx*visual.SlideLightX+ny*visual.SlideLightY+nz*visual.SlideLightZ, 0) / visual.SlideLightZ
	f.shade = visual.SlideShade + (1-visual.SlideShade)*lit
	hx, hy, hz := visual.SlideLightX, visual.SlideLightY, visual.SlideLightZ+1
	mirror := max(nx*hx+ny*hy+nz*hz, 0) / math.Sqrt(hx*hx+hy*hy+hz*hz)
	mirror *= mirror
	mirror *= mirror
	mirror *= mirror
	f.gloss = mirror * mirror * mirror * visual.SlideGloss * f.height
	f.body = color.Scale(color.Lerp(visual.RgbSlideFront, visual.RgbSlideWake, across), f.shade)

	// What lies under is seen through the swell, drawn toward its crest
	bend := visual.SlideLens * slope
	f.under = r.at(ctx, x-bend*outX, y-ctx.CellAspect.FromCircular(bend*outY))
	return f
}

// at is the held frame's cell under a point of the map, the bare background off it
func (r *SlideRenderer) at(ctx render.RenderContext, x, y float64) terminal.Cell {
	sx, sy, visible := ctx.MapToScreen(int(math.Round(x)), int(math.Round(y)))
	if i := sy*r.width + sx; visible && r.width > 0 && sx >= 0 && sx < r.width && i >= 0 && i < len(r.under) {
		return r.under[i]
	}
	return terminal.Cell{Bg: visual.RgbBackground}
}
