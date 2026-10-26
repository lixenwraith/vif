package renderer

import (
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
}

func NewBehaviorRenderer(gameCtx *engine.GameContext) *BehaviorRenderer {
	return &BehaviorRenderer{gameCtx: gameCtx}
}

// Render draws each instance's members from the frame it shows, coloured as the
// swarm is: hit flash, then enraged, then the prototype's colour
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

		c := p.Color
		switch {
		case combat.RemainingHitFlash > 0:
			c = hitFlashColor(combat.RemainingHitFlash)
		case combat.IsEnraged:
			c = visual.RgbCombatEnraged
		}

		frame := p.Frames[b.PatternIndex%len(p.Frames)]
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
			buf.SetFgOnly(screenX, screenY, frame[row*p.Width+col], c, terminal.AttrNone)
		}
		return true
	})
}
