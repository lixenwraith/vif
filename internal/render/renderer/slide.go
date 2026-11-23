package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// SlideRenderer draws each slide pulse as a sheet of tiles struck at its center: the
// cells its edge has just passed are tiles that tilt, turn edge-on and fall flat
// again, lit as they turn, so a ripple of flipping tiles runs out with the pulse
type SlideRenderer struct {
	gameCtx *engine.GameContext
}

func NewSlideRenderer(gameCtx *engine.GameContext) *SlideRenderer {
	return &SlideRenderer{gameCtx: gameCtx}
}

// slideFace is a tile's face by the eighths of its cell it still shows
var slideFace = [...]rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func (r *SlideRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pulses := r.gameCtx.World.Components.Pulse
	if pulses.CountEntities() == 0 {
		return
	}
	buf.SetWriteMask(visual.MaskTransient)
	pulses.Each(func(_ core.Entity, p *component.PulseComponent) bool {
		if p.Style == component.PulseSlide && p.Radius > 0 {
			r.ripple(ctx, buf, p)
		}
		return true
	})
}

// ripple turns the tiles between the pulse's edge and SlideBand behind it, each as
// far round as the edge has left it
func (r *SlideRenderer) ripple(ctx render.RenderContext, buf *render.RenderBuffer, p *component.PulseComponent) {
	front := p.Radius * p.Size()
	startX, endX, startY, endY, ok := effectBounds(ctx, p.X, p.Y, front, ctx.CellAspect.FromCircular(front))
	if !ok {
		return
	}
	for y := startY; y <= endY; y++ {
		dy := ctx.CellAspect.Circular(float64(y - p.Y))
		for x := startX; x <= endX; x++ {
			behind := front - math.Hypot(float64(x-p.X), dy)
			if behind <= 0 || behind >= visual.SlideBand {
				continue
			}
			if sx, sy, visible := ctx.MapToScreen(x, y); visible {
				r.tile(buf, sx, sy, behind/visual.SlideBand)
			}
		}
	}
}

// tile draws one cell a share of the way through its flip: its face shrinks to the
// cell's foot as it tilts into the light, and past edge-on its shaded underside
// comes down from the cell's head
func (r *SlideRenderer) tile(buf *render.RenderBuffer, sx, sy int, turned float64) {
	angle := turned * math.Pi
	face := int(math.Abs(math.Cos(angle))*8 + 0.5)
	glint := math.Sin(2 * angle)
	plate := buf.BackgroundAt(sx, sy, visual.RgbBackground)
	if cell := buf.CellAt(sx, sy); cell.Rune != 0 && cell.Rune != ' ' {
		plate = color.Lerp(plate, cell.Fg, 0.5)
	}
	if turned < 0.5 {
		plate = color.Lerp(plate, visual.RgbSlideLight, visual.SlideShine*glint)
		buf.SetWithBg(sx, sy, slideFace[face], plate, visual.RgbSlideGap)
		return
	}
	plate = color.Scale(plate, 1+visual.SlideShade*glint)
	buf.SetWithBg(sx, sy, slideFace[8-face], visual.RgbSlideGap, plate)
}
