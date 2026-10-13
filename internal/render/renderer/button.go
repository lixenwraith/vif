package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// ButtonRenderer draws each button as an opaque block with its key in the middle, so
// drifting glyphs pass behind it. A lit button fades back to unlit over its light.
type ButtonRenderer struct {
	gameCtx *engine.GameContext
	console *render.Console // nil draws true colour
}

func NewButtonRenderer(ctx *engine.GameContext) *ButtonRenderer {
	r := &ButtonRenderer{gameCtx: ctx}
	if cfg := ctx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.console = render.ConsoleFor(cfg.ConsolePalette)
	}
	return r
}

func (r *ButtonRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	buttons := world.Components.Button
	count := buttons.CountEntities()
	if count == 0 {
		return
	}
	// Composite owns the cell outright and stays out of grayout and the occlusion dim
	buf.SetWriteMask(visual.MaskComposite)
	cfg := world.Resources.Config
	buttons.Each(func(e core.Entity, b *component.ButtonComponent) bool {
		x, y, w, h := component.ButtonRect(b.Slot, count, cfg.MapWidth, cfg.MapHeight)
		lit := r.lit(e, b)
		for dy := range h {
			for dx := range w {
				sx, sy, visible := ctx.MapToScreen(x+dx, y+dy)
				if !visible {
					continue
				}
				label := dx == w/2 && dy == h/2
				if r.console != nil {
					r.cell256(buf, sx, sy, b, lit > 0, label)
				} else {
					r.cellTrueColor(buf, sx, sy, b, lit, label)
				}
			}
		}
		return true
	})
}

// lit is how lit a button shows, 1 to 0 as its light fades. This instance's own press
// shows by its echo, at once; the shared light it crosses lands a playout lead later,
// so it is left to everyone else, as a placement is left to its prediction (D-18).
func (r *ButtonRenderer) lit(e core.Entity, b *component.ButtonComponent) float64 {
	world := r.gameCtx.World
	lit := 0.0
	if b.PressedBy == 0 || !world.Resources.Player.IsLocal(b.PressedBy) {
		if b.Held {
			lit = 1
		} else if t, ok := world.Components.Timer.GetPtr(e); ok && t.Running(component.TimerDeadline) {
			lit = 1 - t.Progress(component.TimerDeadline)
		}
	}
	if echo := world.Resources.View.ButtonEcho; echo.Key == b.Key {
		if echo.Held {
			return 1
		}
		lit = max(lit, float64(echo.Remaining)/float64(parameter.ButtonPressLight))
	}
	return lit
}

func (r *ButtonRenderer) cellTrueColor(buf *render.RenderBuffer, sx, sy int, b *component.ButtonComponent, lit float64, label bool) {
	bg := color.Scale(b.Color, visual.ButtonUnlit+(1-visual.ButtonUnlit)*lit)
	if !label {
		buf.SetWithBg(sx, sy, ' ', bg, bg)
		return
	}
	fg := b.Color
	if lit > 0 {
		fg = visual.RgbBlack
	}
	buf.SetWithBg(sx, sy, b.Key, fg, bg)
}

// cell256 has no fade: a console shows a button lit for as long as it is lit
func (r *ButtonRenderer) cell256(buf *render.RenderBuffer, sx, sy int, b *component.ButtonComponent, lit, label bool) {
	dark, light := r.console.Family(b.Color)
	fill := r.console.Background(dark)
	if lit {
		fill = r.console.Background(light)
	}
	switch {
	case label:
		buf.SetWithBg(sx, sy, b.Key, visual.RgbBlack, visual.RgbBlack)
		buf.SetBg256(sx, sy, fill)
		buf.SetFgOnly(sx, sy, b.Key, color.RGB{R: r.console.TextOn(fill)}, terminal.AttrFg256)
	case lit:
		buf.SetWithBg(sx, sy, visual.ButtonLit256, visual.RgbBlack, visual.RgbBlack)
		buf.SetBg256(sx, sy, fill)
		buf.SetFgOnly(sx, sy, visual.ButtonLit256, color.RGB{R: light}, terminal.AttrFg256)
	default:
		buf.SetWithBg(sx, sy, ' ', visual.RgbBlack, visual.RgbBlack)
		buf.SetBg256(sx, sy, fill)
	}
}
