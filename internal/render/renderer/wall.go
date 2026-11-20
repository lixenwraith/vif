package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

type wallCellRenderer func(buf *render.RenderBuffer, screenX, screenY int,
	char rune, fg, bg color.RGB, renderFg, renderBg bool, attrs terminal.Attr)

// WallRenderer draws wall entities with fg/bg support, and the rock field
type WallRenderer struct {
	gameCtx    *engine.GameContext
	renderCell wallCellRenderer
	stone      stoneColumns
	drawn      []engine.RockColumn // the rock's columns and those ahead of it, this frame
}

func NewWallRenderer(ctx *engine.GameContext) *WallRenderer {
	r := &WallRenderer{
		gameCtx: ctx,
	}

	if ctx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.renderCell256
		r.stone.index256 = true
	} else {
		r.renderCell = r.renderCellTrueColor
	}

	return r
}

// Render draws the rock and the walls where the cave's scroll stands at the frame's
// moment: rock and shared walls ride it, carried on from their cells by the part of a
// tick since, as the drift's layers are
func (r *WallRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	walls := r.gameCtx.World.Components.Wall
	rock := r.gameCtx.World.Positions.Rock()
	if walls.CountEntities() == 0 && rock == nil {
		return
	}
	buf.SetWriteMask(visual.MaskWall)
	carry := rockCarry(ctx, r.gameCtx.World)
	if rock != nil {
		r.renderRock(&ctx, buf, rock, carry)
	}
	fade, risen := wallFadeIn(ctx, r.gameCtx.World)

	walls.Each(func(wallEntity core.Entity, wallComp *component.WallComponent) bool {
		if !(wallComp.RenderFg || wallComp.RenderBg) {
			return true
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(wallEntity)
		if !ok {
			return true
		}
		if wallEntity.Domain() == core.DomainShared {
			pos.X -= carry
		}

		// Transform map coords to screen coords with visibility check
		screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
		if !visible {
			return true
		}

		if wallEntity >= fade.First && wallEntity <= fade.Last {
			r.renderRising(buf, screenX, screenY, wallComp, risen)
			return true
		}
		r.renderCell(buf, screenX, screenY, wallComp.Rune, wallComp.FgColor, wallComp.BgColor,
			wallComp.RenderFg, wallComp.RenderBg, wallComp.Attrs)
		return true
	})
}

// renderRising draws a wall risen this far from the ground already drawn under it
func (r *WallRenderer) renderRising(buf *render.RenderBuffer, screenX, screenY int, wall *component.WallComponent, risen float64) {
	under := buf.BackgroundAt(screenX, screenY, visual.RgbBackground)
	fg, bg := visual.WallRGB(wall)
	r.renderCell(buf, screenX, screenY, wall.Rune, color.Blend(under, fg, risen), color.Blend(under, bg, risen),
		wall.RenderFg, wall.RenderBg, wall.Attrs&^(terminal.AttrFg256|terminal.AttrBg256))
}

// wallFadeIn is the walls fading in at the frame's moment and how far they have
// risen from the ground under them; the zero run, once they stand, names no wall
func wallFadeIn(ctx render.RenderContext, world *engine.World) (component.WallFadeIn, float64) {
	fade := world.Resources.Transient.WallFadeIn
	if fade.Duration <= 0 {
		return component.WallFadeIn{}, 1
	}
	since, _ := sinceTick(ctx, world.Resources.Time)
	risen := float64(fade.Elapsed+since) / float64(fade.Duration)
	if risen >= 1 {
		return component.WallFadeIn{}, 1
	}
	return fade, risen
}

// rockCarry is how many columns the cave's scroll stands past its tick at the frame's
// moment, as far as the columns ahead of the rock reach
func rockCarry(ctx render.RenderContext, world *engine.World) int {
	rock := world.Positions.Rock()
	since, ok := sinceTick(ctx, world.Resources.Time)
	if rock == nil || !ok {
		return 0
	}
	return min(world.Resources.Cave.Carry(since), len(rock.Ahead()))
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
	return color.RGBTo256(c)
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
