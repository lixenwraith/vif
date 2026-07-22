package renderer

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
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
	entities := r.gameCtx.World.Components.Drain.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, entity := range entities {
		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(entity)
		if !ok {
			continue
		}

		// Priority: hit flash > enraged > normal
		var c color.RGB
		switch {
		case combatComp.RemainingHitFlash > 0:
			c = r.calculateFlashColor(combatComp.RemainingHitFlash)
		case combatComp.IsEnraged:
			c = visual.RgbCombatEnraged
		default:
			c = visual.RgbDrain
		}

		buf.SetFgOnly(screenX, screenY, visual.DrainChar, c, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect
func (r *DrainRenderer) calculateFlashColor(remaining time.Duration) color.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return color.Scale(visual.RgbCombatHitFlash, intensity)
}
