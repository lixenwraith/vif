package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
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
		var color terminal.RGB
		switch {
		case combatComp.RemainingHitFlash > 0:
			color = r.calculateFlashColor(combatComp.RemainingHitFlash)
		case combatComp.IsEnraged:
			color = visual.RgbCombatEnraged
		default:
			color = visual.RgbDrain
		}

		buf.SetFgOnly(screenX, screenY, visual.DrainChar, color, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect
func (r *DrainRenderer) calculateFlashColor(remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return render.Scale(visual.RgbCombatHitFlash, intensity)
}