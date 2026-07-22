package renderer

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
)

// swarmCellRenderer callback forcolor.RGB animated ASCII composite rendering (256 vs TrueColor)
type swarmCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// SwarmRenderer draws the swarm composite
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
		var c color.RGB
		switch {
		case combatComp.RemainingHitFlash > 0:
			c = r.calculateFlashColor(combatComp.RemainingHitFlash)
		case combatComp.IsEnraged:
			c = visual.RgbCombatEnraged
		default:
			c = visual.RgbDrain
		}

		r.renderMembers(ctx, buf, &headerComp, &swarmComp, c)
	}
}

// renderMembers draws swarm members based on current pattern
func (r *SwarmRenderer) renderMembers(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	headerComp *component.HeaderComponent,
	swarmComp *component.SwarmComponent,
	c color.RGB,
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

		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			continue
		}

		// Convert offset to pattern array indices
		row := member.OffsetY + parameter.SwarmHeaderOffsetY
		col := member.OffsetX + parameter.SwarmHeaderOffsetX

		if row < 0 || row >= parameter.SwarmHeight || col < 0 || col >= parameter.SwarmWidth {
			continue
		}

		ch := visual.SwarmPatternChars[patternIdx][row][col]
		buf.SetFgOnly(screenX, screenY, ch, c, terminal.AttrNone)
	}
}

// calculateFlashColor returns yellow with pulse effect
func (r *SwarmRenderer) calculateFlashColor(remaining time.Duration) color.RGB {
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
