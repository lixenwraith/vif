package renderer

import (
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

type wallCellRenderer func(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool, attrs terminal.Attr)

// WallRenderer draws wall entities with fg/bg support
type WallRenderer struct {
	gameCtx    *engine.GameContext
	renderCell wallCellRenderer
}

func NewWallRenderer(ctx *engine.GameContext) *WallRenderer {
	r := &WallRenderer{
		gameCtx: ctx,
	}

	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
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

	for _, wallEntity := range wallEntities {
		wallComp, ok := r.gameCtx.World.Components.Wall.GetComponent(wallEntity)
		if !ok {
			continue
		}

		if !(wallComp.RenderFg || wallComp.RenderBg) {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(wallEntity)
		if !ok {
			continue
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			continue
		}

		r.renderCell(buf, screenX, screenY, wallComp.Rune, wallComp.FgColor, wallComp.BgColor,
			wallComp.RenderFg, wallComp.RenderBg, wallComp.Attrs)
	}
}

func (r *WallRenderer) renderCellTrueColor(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool, attrs terminal.Attr) {

	if renderFg && renderBg {
		buf.SetWithBg(screenX, screenY, char, fg, bg)
	} else if renderFg {
		buf.SetFgOnly(screenX, screenY, char, fg, terminal.AttrNone)
	} else if renderBg {
		buf.SetBgOnly(screenX, screenY, bg)
	}
}

// renderCell256 updated to use per-cell colors with fallback
func (r *WallRenderer) renderCell256(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg terminal.RGB, renderFg, renderBg bool, attrs terminal.Attr) {

	if renderBg {
		// Use per-cell palette index if set, otherwise fallback to default
		// In 256 mode, palette index stored in RGB.R
		var paletteIdx uint8
		if attrs&terminal.AttrBg256 != 0 {
			paletteIdx = bg.R
		} else {
			paletteIdx = terminal.RGBTo256(bg)
		}
		buf.SetBg256(screenX, screenY, paletteIdx)
	}

	if renderFg && char != 0 {
		// Use per-cell fg palette index if set
		var fgIdx uint8
		if attrs&terminal.AttrFg256 != 0 {
			fgIdx = fg.R
		} else {
			fgIdx = terminal.RGBTo256(fg)
		}
		buf.SetFgOnly(screenX, screenY, char,
			terminal.RGB{R: fgIdx}, terminal.AttrFg256)
	}
}