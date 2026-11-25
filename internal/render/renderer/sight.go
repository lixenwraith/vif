package renderer

import (
	"cmp"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SightRenderer is the darkness a scenario lays over the map and the light the
// moving cursor throws through it: a wedge a right angle wide toward its step,
// which walls stop, leaving what it fell on lit a moment. It reads the light from
// the cursor's own steps, so a replay and a spectator see it as its player did.
type SightRenderer struct {
	gameCtx *engine.GameContext

	width, height int
	lit           []int64 // the game time, in ns, each map cell was last lit; 0 for never
	last          int64   // the frame's game time, which a new game sets back

	at    vmath.Point
	seen  bool
	way   [2]int    // the way the cursor's last step went
	moved time.Time // when
	trail []vmath.Point
}

func NewSightRenderer(ctx *engine.GameContext) *SightRenderer {
	return &SightRenderer{gameCtx: ctx}
}

// Render follows the cursor's steps every frame, so a trail holds the walk begun in
// the dark, then darkens what its light has not reached and draws the trail
func (r *SightRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	now := ctx.GameTime.UnixNano()
	r.follow(&ctx, now)
	sight := r.gameCtx.World.Resources.View.Sight
	if sight.Dark {
		r.cast(&ctx, now)
		r.shade(&ctx, buf, now)
	}
	if sight.Trail {
		r.drawTrail(&ctx, buf)
	}
}

// follow takes the cursor's cell: a step or a few along one axis carry the walk on,
// anything else, a spawn or a jump, starts a new one there
func (r *SightRenderer) follow(ctx *render.RenderContext, now int64) {
	if ctx.MapWidth != r.width || ctx.MapHeight != r.height || now < r.last {
		r.width, r.height = ctx.MapWidth, ctx.MapHeight
		r.lit = make([]int64, r.width*r.height)
		r.seen = false
	}
	r.last = now
	if !ctx.CursorValid {
		return
	}
	at := vmath.Point{X: ctx.CursorX, Y: ctx.CursorY}
	dx, dy := at.X-r.at.X, at.Y-r.at.Y
	switch {
	case r.seen && dx == 0 && dy == 0:
		return
	case r.seen && (dx == 0 || dy == 0) && vmath.IntAbs(dx+dy) <= visual.SightStepReach:
		r.way, r.moved = [2]int{cmp.Compare(dx, 0), cmp.Compare(dy, 0)}, ctx.GameTime
		for p := r.at; p != at; {
			p.X, p.Y = p.X+r.way[0], p.Y+r.way[1]
			r.trail = append(r.trail, p)
		}
	default:
		r.way, r.trail = [2]int{}, append(r.trail[:0], at)
	}
	r.at, r.seen = at, true
}

// cast lights what the cursor's light falls on while it steps: a right angle on
// the screen, its sides diagonal to the screen's edges, whatever the cell's shape
func (r *SightRenderer) cast(ctx *render.RenderContext, now int64) {
	if r.way == ([2]int{}) || ctx.GameTime.Sub(r.moved) > visual.SightLightHold {
		return
	}
	aspect := float64(ctx.CellAspect)
	if aspect <= 0 {
		aspect = float64(vmath.RefAspect)
	}
	spread := aspect
	if r.way[0] == 0 {
		spread = 1 / aspect
	}
	positions := r.gameCtx.World.Positions
	inside := func(x, y int) bool { return x >= 0 && y >= 0 && x < r.width && y < r.height }
	vmath.ShadowCast(r.at.X, r.at.Y, r.way[0], r.way[1], spread, max(r.width, r.height),
		func(x, y int) bool {
			return !inside(x, y) || positions.IsBlocked(vmath.Point{X: x, Y: y}, component.WallBlockCursor)
		},
		func(x, y int) {
			if inside(x, y) {
				r.lit[y*r.width+x] = now
			}
		})
}

// shade darkens each shown cell by how long ago the light left it
func (r *SightRenderer) shade(ctx *render.RenderContext, buf *render.RenderBuffer, now int64) {
	minX, minY, maxX, maxY := ctx.VisibleMapBounds()
	fade := float64(visual.SightFade)
	for y := minY; y <= maxY && y < r.height; y++ {
		for x := minX; x <= maxX && x < r.width; x++ {
			keep := 0.0
			if at := r.lit[y*r.width+x]; at != 0 {
				keep = 1 - float64(now-at)/fade
			}
			if sx, sy, ok := ctx.MapToScreen(x, y); ok {
				buf.Shade(sx, sy, keep, visual.RgbBackground)
			}
		}
	}
}

// drawTrail draws the cells the cursor walked, where it began marked
func (r *SightRenderer) drawTrail(ctx *render.RenderContext, buf *render.RenderBuffer) {
	for i, p := range r.trail {
		mark, rgb := visual.SightTrailRune, visual.RgbSightTrail
		if i == 0 {
			mark, rgb = visual.SightStartRune, visual.RgbSightStart
		}
		if sx, sy, ok := ctx.MapToScreen(p.X, p.Y); ok {
			buf.SetFgOnly(sx, sy, mark, rgb, terminal.AttrNone)
		}
	}
}
