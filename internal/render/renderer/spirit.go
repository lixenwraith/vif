package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SpiritRenderer draws the spirit flights: converging parts spiralling in to their
// targets with a trail of light, scattered ones flying out and fading
type SpiritRenderer struct {
	gameCtx *engine.GameContext
}

func NewSpiritRenderer(gameCtx *engine.GameContext) *SpiritRenderer {
	return &SpiritRenderer{
		gameCtx: gameCtx,
	}
}

func (r *SpiritRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	spirits := world.Components.Spirit
	if spirits.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	spirits.Each(func(e core.Entity, spirit *component.SpiritComponent) bool {
		progress, ok := deadlineProgress(ctx, world, e)
		if !ok {
			return true
		}
		if spirit.Pattern == component.SpiritScatter {
			drawScatter(ctx, buf, spirit.Parts, progress)
		} else {
			drawConverge(ctx, buf, spirit, progress)
		}
		return true
	})
}

// drawScatter draws each part once on its way out, fast at first and slowing, its
// light going as it settles. A cell already showing a glyph keeps it: a scatter
// passes under what is there, as the species drawn after it pass over it. The
// blank a crosshair or a grid line leaves in a cell is no glyph.
func drawScatter(ctx render.RenderContext, buf *render.RenderBuffer, parts []component.SpiritPart, progress float64) {
	rest := 1 - progress
	travelled, light := 1-rest*rest, 1-progress*progress
	for i := range parts {
		part := &parts[i]
		at := vmath.PointAt(vmath.Lerp(part.FromX, part.ToX, travelled), vmath.Lerp(part.FromY, part.ToY, travelled))
		screenX, screenY, visible := ctx.MapToScreen(at.X, at.Y)
		if !visible {
			continue
		}
		if under := buf.CellAt(screenX, screenY).Rune; under != 0 && under != ' ' {
			continue
		}
		buf.SetFgOnly(screenX, screenY, part.Rune, color.Scale(part.Color, light), terminal.AttrNone)
	}
}

// drawConverge draws each part's head and the segments trailing it down the spiral
// it rides in to its target. The parts keep one time, so each segment's turn, pull
// and light are worked out once for the flight.
func drawConverge(ctx render.RenderContext, buf *render.RenderBuffer, spirit *component.SpiritComponent, progress float64) {
	var segments [visual.SpiritTrailSteps]struct {
		cos, sin, pull, alpha float64
		c                     color.RGB
	}
	drawn := 0
	for step := range segments {
		// Each segment rides the same spiral a lag behind the one ahead
		p := progress - float64(step)*visual.SpiritTrailLag
		if p < 0 {
			break
		}
		seg := &segments[step]
		seg.cos, seg.sin, seg.pull = vmath.Cos(p*visual.SpiritSpin), vmath.Sin(p*visual.SpiritSpin), 1-p
		// The head brightens as it nears the target; a trail segment's color lags
		// its place once more, and it dims down the trail's length
		seg.c, seg.alpha = color.Scale(spiritProgressColor(spirit.Color, p), vmath.Lerp(visual.SpiritHeadFloor, 1, p)), 1
		if step > 0 {
			fade := min((1-float64(step)/float64(visual.SpiritTrailSteps))*visual.SpiritTrailBoost, 1)
			seg.c = color.Scale(spiritProgressColor(spirit.Color, max(p-float64(step-1)*visual.SpiritTrailLag, 0)), fade)
			seg.alpha = vmath.Lerp(visual.SpiritTrailAlphaFloor, 1, fade)
		}
		drawn++
	}

	for i := range spirit.Parts {
		part := &spirit.Parts[i]
		// The spiral turns in circular space, so it reads round on tall cells, and
		// one way or the other by the start cell, so an implosion reads as chaotic
		relX, relY := part.FromX-part.ToX, ctx.CellAspect.Circular(part.FromY-part.ToY)
		way := 1.0
		if start := vmath.PointAt(part.FromX, part.FromY); (start.X^start.Y)&1 != 0 {
			way = -1
		}
		for step := range drawn {
			seg := &segments[step]
			sin := way * seg.sin
			at := vmath.PointAt(part.ToX+(relX*seg.cos-relY*sin)*seg.pull,
				part.ToY+ctx.CellAspect.FromCircular((relX*sin+relY*seg.cos)*seg.pull))
			if screenX, screenY, visible := ctx.MapToScreen(at.X, at.Y); visible {
				buf.Set(screenX, screenY, part.Rune, seg.c, visual.RgbBlack, render.BlendAddFg, seg.alpha, terminal.AttrNone)
			}
		}
	}
}

// spiritProgressColor is a converging spirit's color at a progress: its base's
// place on the heat gradient, cycled on through the gradient's span
func spiritProgressColor(base component.SpiritColor, progress float64) color.RGB {
	return render.HeatGradientLUT[(visual.SpiritBaseOffsets[base]+int(progress*visual.SpiritGradientSpan))&0xFF]
}
