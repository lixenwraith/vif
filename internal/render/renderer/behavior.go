package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// BehaviorRenderer draws the data species, each body from its program's prototype
type BehaviorRenderer struct {
	gameCtx *engine.GameContext
	paint   lookPainter
}

func NewBehaviorRenderer(gameCtx *engine.GameContext) *BehaviorRenderer {
	return &BehaviorRenderer{gameCtx: gameCtx, paint: newLookPainter(gameCtx.World.Resources.Config.ColorMode)}
}

// Render draws each instance's members from the frame it shows in its look, the hit
// flash, then the enraged colour, tinting every glyph
func (r *BehaviorRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	if world.Components.Behavior.CountEntities() == 0 {
		return
	}
	prototypes := world.Resources.Behaviors

	buf.SetWriteMask(visual.MaskComposite)

	world.Components.Behavior.Each(func(header core.Entity, b *component.BehaviorComponent) bool {
		h, hasHeader := world.Components.Header.GetPtr(header)
		combat, hasCombat := world.Components.Combat.GetPtr(header)
		if !hasHeader || !hasCombat || int(b.Program) >= len(prototypes) {
			return true
		}
		p := &prototypes[b.Program]

		var tint color.RGB
		hit := combat.RemainingHitFlash > 0
		switch {
		case hit:
			tint = hitFlashColor(combat.RemainingHitFlash)
		case combat.IsEnraged:
			tint = visual.RgbCombatEnraged
		}

		frame := p.Look.Frames[b.PatternIndex%len(p.Look.Frames)]
		for _, member := range h.MemberEntries {
			if member.Entity == 0 {
				continue
			}
			pos, ok := world.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}
			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}
			row, col := member.OffsetY+p.HeaderY, member.OffsetX+p.HeaderX
			if row < 0 || row >= p.Height || col < 0 || col >= p.Width {
				continue
			}
			r.paint.cell(buf, screenX, screenY, &p.Look, &frame[row*p.Width+col], tint, hit || combat.IsEnraged, hit)
		}
		return true
	})
}

// lookPainter draws a look's cells, a data species' and an eye's alike: a cell's
// background, then its glyph in its palette colour or in tint where tinted. A
// 256-colour host draws a look's console pair where it has one, which reads on
// every console palette, and shows a hit by reversing the glyph.
type lookPainter struct{ console bool }

func newLookPainter(mode terminal.ColorMode) lookPainter {
	return lookPainter{console: mode == terminal.ColorMode256}
}

func (p lookPainter) cell(buf *render.RenderBuffer, x, y int, look *component.Look, cell *component.LookCell, tint color.RGB, tinted, hit bool) {
	if p.console && look.Console {
		if cell.Bg >= 0 {
			buf.SetBg256(x, y, look.Bg256)
		}
		if cell.Rune == 0 {
			return
		}
		fg, attr := color.RGB{R: look.Fg256}, terminal.AttrFg256|cell.Attr
		switch {
		case hit:
			attr |= terminal.AttrReverse
		case tinted:
			fg, attr = tint, cell.Attr
		}
		buf.SetFgOnly(x, y, cell.Rune, fg, attr)
		return
	}
	if cell.Bg >= 0 {
		buf.SetBgOnly(x, y, look.Bg[cell.Bg])
	}
	if cell.Rune == 0 {
		return
	}
	fg := look.Color
	switch {
	case tinted:
		fg = tint
	case cell.Fg >= 0:
		fg = look.Fg[cell.Fg]
	}
	buf.SetFgOnly(x, y, cell.Rune, fg, cell.Attr)
}
