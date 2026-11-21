package renderer

import (
	"strconv"
	"unicode/utf8"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/machine"
)

// MachineRenderer labels each instance of a species a behavior machine steps, the
// swarm, the quasar, a storm circle and every data species, with the state it
// stands in and how far through a timed one it is
type MachineRenderer struct {
	gameCtx *engine.GameContext
}

func NewMachineRenderer(gameCtx *engine.GameContext) *MachineRenderer {
	return &MachineRenderer{gameCtx: gameCtx}
}

// IsVisible implements render.VisibilityToggle
func (r *MachineRenderer) IsVisible() bool { return r.gameCtx.OverlayMachine.Load() }

func (r *MachineRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	c := &r.gameCtx.World.Components
	buf.SetWriteMask(visual.MaskUI)
	c.Swarm.Each(func(e core.Entity, s *component.SwarmComponent) bool {
		return label(r, ctx, buf, e, component.SpeciesSwarm, s.Machine)
	})
	c.Quasar.Each(func(e core.Entity, q *component.QuasarComponent) bool {
		return label(r, ctx, buf, e, component.SpeciesQuasar, q.Machine)
	})
	c.StormCircle.Each(func(e core.Entity, circle *component.StormCircleComponent) bool {
		return label(r, ctx, buf, e, component.SpeciesStorm, circle.Machine)
	})
	c.Behavior.Each(func(e core.Entity, b *component.BehaviorComponent) bool {
		return label(r, ctx, buf, e, component.SpeciesDataFirst+component.SpeciesType(b.Program), b.Machine)
	})
}

// label draws an instance's state centred on the row above its body, under it at
// the map's top, and kept inside the playfield; a header off the screen gets none
func label[S ~uint8](r *MachineRenderer, ctx render.RenderContext, buf *render.RenderBuffer, e core.Entity, species component.SpeciesType, in machine.Instance[S]) bool {
	res := r.gameCtx.World.Resources
	pos, ok := r.gameCtx.World.Positions.GetPosition(e)
	if !ok || int(species) >= len(res.Machines) || int(in.State) >= len(res.Machines[species]) || !ctx.IsInViewport(pos.X, pos.Y) {
		return true
	}
	text := res.Machines[species][in.State]
	if in.Duration > 0 {
		text += " " + strconv.Itoa(int(in.Progress()*100)) + "%"
	}
	foot, width := res.Footprint(species), utf8.RuneCountInString(text)
	top := pos.Y - foot.HeaderY
	y := top - 1
	if y < 0 {
		y = top + foot.Height
	}
	x := pos.X - foot.HeaderX + (foot.Width-width)/2

	field := ctx.PlayfieldRect()
	sx, sy := ctx.ViewportToScreen(x-ctx.CameraX+ctx.MapOffsetX, y-ctx.CameraY+ctx.MapOffsetY)
	if sy < field.Y0 || sy >= field.Y1 {
		return true
	}
	sx = max(min(sx, field.X1-width), field.X0)
	fillHUDRect(buf, sx, sy, width, 1)
	drawFg(buf, sx, sy, text, visual.RgbHudValue, terminal.AttrNone)
	return true
}
