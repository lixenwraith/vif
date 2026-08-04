package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

type wallCellRenderer func(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg color.RGB, renderFg, renderBg bool, attrs terminal.Attr)

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
	walls := r.gameCtx.World.Components.Wall
	if walls.CountEntities() == 0 {
		return
	}

	walls.Each(func(wallEntity core.Entity, wallComp *component.WallComponent) bool {
		if !(wallComp.RenderFg || wallComp.RenderBg) {
			return true
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(wallEntity)
		if !ok {
			return true
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		r.renderCell(buf, screenX, screenY, wallComp.Rune, wallComp.FgColor, wallComp.BgColor,
			wallComp.RenderFg, wallComp.RenderBg, wallComp.Attrs)
		return true
	})
}

func (r *WallRenderer) renderCellTrueColor(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg color.RGB, renderFg, renderBg bool, attrs terminal.Attr) {

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
	char rune, fg, bg color.RGB, renderFg, renderBg bool, attrs terminal.Attr) {

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
			color.RGB{R: fgIdx}, terminal.AttrFg256)
	}
}
