// FILE: render/renderers/quasar.go
package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// QuasarRenderer draws the quasar boss entity
type QuasarRenderer struct {
	gameCtx *engine.GameContext

	quasarStore *engine.Store[component.QuasarComponent]
	headerStore *engine.Store[component.CompositeHeaderComponent]
	charStore   *engine.Store[component.CharacterComponent]
}

// NewQuasarRenderer creates a new quasar renderer
func NewQuasarRenderer(gameCtx *engine.GameContext) *QuasarRenderer {
	return &QuasarRenderer{
		gameCtx: gameCtx,

		quasarStore: engine.GetStore[component.QuasarComponent](gameCtx.World),
		headerStore: engine.GetStore[component.CompositeHeaderComponent](gameCtx.World),
		charStore:   engine.GetStore[component.CharacterComponent](gameCtx.World),
	}
}

// Render draws the quasar composite entity
func (r *QuasarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	// Find active quasar anchors
	anchors := r.quasarStore.All()
	if len(anchors) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskComposite)

	for _, anchor := range anchors {
		header, ok := r.headerStore.Get(anchor)
		if !ok {
			continue
		}

		// Render each member
		for _, member := range header.Members {
			if member.Entity == 0 {
				continue
			}

			pos, hasPos := r.gameCtx.World.Positions.Get(member.Entity)
			if !hasPos {
				continue
			}

			char, hasChar := r.charStore.Get(member.Entity)
			if !hasChar {
				continue
			}

			screenX := ctx.GameX + pos.X
			screenY := ctx.GameY + pos.Y

			// Bounds check
			if screenX < ctx.GameX || screenX >= ctx.Width ||
				screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
				continue
			}

			buf.SetFgOnly(screenX, screenY, char.Rune, render.RgbDrain, terminal.AttrNone)
		}
	}
}