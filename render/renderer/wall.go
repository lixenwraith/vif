package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

type wallCellRenderer func(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool)

// WallRenderer draws wall entities with fg/bg support
type WallRenderer struct {
	gameCtx    *engine.GameContext
	renderCell wallCellRenderer
}

func NewWallRenderer(ctx *engine.GameContext) *WallRenderer {
	r := &WallRenderer{
		gameCtx: ctx,
	}

	if ctx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.renderCell256
	} else {
		r.renderCell = r.renderCellTrueColor
	}

	return r
}

func (r *WallRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	wallEntities := r.gameCtx.World.Components.Wall.GetAllEntities()
	if len(wallEntities) == 0 {
		return
	}

	for _, entity := range wallEntities {
		wall, ok := r.gameCtx.World.Components.Wall.GetComponent(entity)
		if !ok || !wall.NeedsRender() {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		screenX := ctx.GameXOffset + pos.X
		screenY := ctx.GameYOffset + pos.Y

		if screenX < ctx.GameXOffset || screenX >= ctx.GameXOffset+ctx.GameWidth ||
			screenY < ctx.GameYOffset || screenY >= ctx.GameYOffset+ctx.GameHeight {
			continue
		}

		r.renderCell(buf, screenX, screenY, wall.Char, wall.FgColor, wall.BgColor, wall.RenderFg, wall.RenderBg)
	}
}

func (r *WallRenderer) renderCellTrueColor(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool) {

	if renderFg && renderBg {
		buf.SetWithBg(screenX, screenY, char, fg, bg)
	} else if renderFg {
		buf.SetFgOnly(screenX, screenY, char, fg, terminal.AttrNone)
	} else if renderBg {
		buf.SetBgOnly(screenX, screenY, bg)
	}
}

func (r *WallRenderer) renderCell256(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool) {

	if renderBg {
		buf.SetBg256(screenX, screenY, visual.Wall256PaletteDefault)
	}

	if renderFg && char != 0 {
		buf.SetFgOnly(screenX, screenY, char,
			terminal.RGB{R: visual.Wall256PaletteDefault}, terminal.AttrFg256)
	}
}