package renderers

import (
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// swarmCellRenderer callback for RGB animated ASCII composite rendering (256 vs TrueColor)
type swarmCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// SwarmRenderer draws the quasar boss entity with optional shield halo
type SwarmRenderer struct {
	gameCtx *engine.GameContext

	// Shield rendering strategy selected at init
	renderSwarmCells swarmCellRenderer
}

// NewSwarmRenderer creates renderer with color-mode-specific shield strategy
func NewSwarmRenderer(gameCtx *engine.GameContext) *SwarmRenderer {
	r := &SwarmRenderer{
		gameCtx: gameCtx,
	}

	return r
}

// Render draws live swarm entities
func (r *SwarmRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	anchors := r.gameCtx.World.Components.Swarm.AllEntities()
	if len(anchors) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, _ = range anchors {

	}
}