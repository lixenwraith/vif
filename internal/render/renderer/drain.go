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

// DrainRenderer draws drain entities with combat-state-aware coloring
type DrainRenderer struct {
	gameCtx *engine.GameContext
}

// NewDrainRenderer creates a new drain renderer
func NewDrainRenderer(gameCtx *engine.GameContext) *DrainRenderer {
	return &DrainRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all drain entities
func (r *DrainRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	drains := r.gameCtx.World.Components.Drain
	if drains.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	drains.Each(func(entity core.Entity, _ *component.DrainComponent) bool {
		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetPtr(entity)
		if !ok {
			return true
		}

		// Priority: hit flash > enraged > normal
		var c color.RGB
		switch {
		case combatComp.RemainingHitFlash > 0:
			c = hitFlashColor(combatComp.RemainingHitFlash)
		case combatComp.IsEnraged:
			c = visual.RgbCombatEnraged
		default:
			c = visual.RgbDrain
		}

		buf.SetFgOnly(screenX, screenY, visual.DrainChar, c, terminal.AttrNone)
		return true
	})
}
