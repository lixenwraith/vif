package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// swarmCellRenderer callback for RGB animated ASCII composite rendering (256 vs TrueColor)
type swarmCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// SwarmRenderer draws the quasar boss entity with optional shield halo
type SwarmRenderer struct {
	gameCtx *engine.GameContext

	// Shield rendering strategy selected at init
	renderSwarmCells swarmCellRenderer
}

// NewSwarmRenderer creates a new swarm renderer
func NewSwarmRenderer(gameCtx *engine.GameContext) *SwarmRenderer {
	return &SwarmRenderer{
		gameCtx: gameCtx,
	}
}

// Render draws all active swarm entities
func (r *SwarmRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Swarm.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, headerEntity := range headerEntities {
		swarmComp, ok := r.gameCtx.World.Components.Swarm.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Determine color: hit flash > enraged > normal
		var color render.RGB
		switch {
		case combatComp.RemainingHitFlash > 0:
			color = r.calculateFlashColor(combatComp.RemainingHitFlash)
		case combatComp.IsEnraged:
			color = render.RgbCombatEnraged
		default:
			color = render.RgbDrain
		}

		r.renderMembers(ctx, buf, &headerComp, &swarmComp, color)
	}
}

// renderMembers draws swarm members based on current pattern
func (r *SwarmRenderer) renderMembers(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	headerComp *component.HeaderComponent,
	swarmComp *component.SwarmComponent,
	color render.RGB,
) {
	patternIdx := swarmComp.PatternIndex
	if patternIdx < 0 || patternIdx >= parameter.SwarmPatternCount {
		patternIdx = 0
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
		if !ok {
			continue
		}

		screenX := ctx.GameXOffset + pos.X
		screenY := ctx.GameYOffset + pos.Y

		if screenX < ctx.GameXOffset || screenX >= ctx.ScreenWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		// Convert offset to pattern array indices
		row := member.OffsetY + parameter.SwarmHeaderOffsetY
		col := member.OffsetX + parameter.SwarmHeaderOffsetX

		if row < 0 || row >= parameter.SwarmHeight || col < 0 || col >= parameter.SwarmWidth {
			continue
		}

		ch := component.SwarmPatternChars[patternIdx][row][col]
		buf.SetFgOnly(screenX, screenY, ch, color, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect
func (r *SwarmRenderer) calculateFlashColor(remaining time.Duration) render.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return render.Scale(render.RgbCombatHitFlash, intensity)
}