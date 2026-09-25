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

// renderCell256 draws a wall cell from its palette indices, taking the nearest where it has RGB
func (r *WallRenderer) renderCell256(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg color.RGB, renderFg, renderBg bool, attrs terminal.Attr) {

	fgIdx, bgIdx := wallIndex256(fg, attrs&terminal.AttrFg256 != 0), wallIndex256(bg, attrs&terminal.AttrBg256 != 0)
	char, swap := wallShade256(char)
	if swap {
		if renderFg && renderBg {
			fgIdx, bgIdx = bgIdx, fgIdx
		} else {
			char = '▒'
		}
	}

	if renderBg {
		buf.SetBg256(screenX, screenY, bgIdx)
	}
	if renderFg && char != 0 {
		buf.SetFgOnly(screenX, screenY, char, color.RGB{R: fgIdx}, terminal.AttrFg256)
	}
}

func wallIndex256(c color.RGB, index bool) uint8 {
	if index {
		return c.R
	}
	return terminal.RGBTo256(c)
}

// wallShade256 is the shade mixing a quadrant block's two colors in the share it fills, since
// console fonts lack quadrants: a quarter ░, a half ▒, three quarters ░ with the colors swapped
func wallShade256(char rune) (shade rune, swap bool) {
	switch char {
	case '▘', '▝', '▖', '▗':
		return '░', false
	case '▀', '▄', '▌', '▐', '▚', '▞':
		return '▒', false
	case '▛', '▜', '▙', '▟', '▓':
		return '░', true
	}
	return char, false
}
