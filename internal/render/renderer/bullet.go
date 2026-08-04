package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
)

type bulletRenderFunc func(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	mapX, mapY int,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
)

type BulletRenderer struct {
	gameCtx      *engine.GameContext
	renderBullet bulletRenderFunc
}

func NewBulletRenderer(gameCtx *engine.GameContext) *BulletRenderer {
	r := &BulletRenderer{gameCtx: gameCtx}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderBullet = r.renderBullet256
	} else {
		r.renderBullet = r.renderBulletTrueColor
	}
	return r
}

func (r *BulletRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	bullets := r.gameCtx.World.Components.Bullet
	if bullets.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	bullets.Each(func(e core.Entity, bullet *component.BulletComponent) bool {
		pos, ok := r.gameCtx.World.Positions.GetPosition(e)
		if !ok {
			return true
		}
		kinetic, ok := r.gameCtx.World.Components.Kinetic.GetPtr(e)
		if !ok {
			return true
		}
		r.renderBullet(ctx, buf, pos.X, pos.Y, kinetic, bullet)
		return true
	})
}

func (r *BulletRenderer) renderBulletTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	mapX, mapY int,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
) {
	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if !visible {
		return
	}

	lifetimeRatio := float64(bullet.Lifetime) / float64(bullet.MaxLifetime)

	// Fade alpha in final 30%
	alpha := 1.0
	if lifetimeRatio > 0.7 {
		alpha = 1.0 - (lifetimeRatio-0.7)/0.3
	}

	// Color dims over lifetime
	c := visual.RgbBulletStormRed
	if lifetimeRatio > 0.5 {
		t := (lifetimeRatio - 0.5) / 0.5
		c = color.Lerp(visual.RgbBulletStormRed, visual.RgbBulletStormRedDim, t)
	}

	char := r.directionChar(kinetic.VelX, kinetic.VelY)
	buf.Set(screenX, screenY, char, c, visual.RgbBlack, render.BlendAddFg, alpha, terminal.AttrBold)
}

func (r *BulletRenderer) renderBullet256(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	mapX, mapY int,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
) {
	screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
	if !visible {
		return
	}

	// Binary visibility: hide in final 20%
	lifetimeRatio := float64(bullet.Lifetime) / float64(bullet.MaxLifetime)
	if lifetimeRatio > 0.8 {
		return
	}

	char := r.directionChar256(kinetic.VelX, kinetic.VelY)
	buf.SetFgOnly(screenX, screenY, char, color.RGB{R: visual.Bullet256StormRed}, terminal.AttrFg256|terminal.AttrBold)
}

func (r *BulletRenderer) directionChar(velX, velY int64) rune {
	if velX == 0 && velY == 0 {
		return '•'
	}

	absX, absY := velX, velY
	if absX < 0 {
		absX = -absX
	}
	if absY < 0 {
		absY = -absY
	}

	threshold := absX / 2

	if absY < threshold {
		if velX > 0 {
			return '▸'
		}
		return '◂'
	}
	if absX < threshold {
		if velY > 0 {
			return '▾'
		}
		return '▴'
	}

	if velX > 0 {
		if velY > 0 {
			return '◢'
		}
		return '◥'
	}
	if velY > 0 {
		return '◣'
	}
	return '◤'
}

func (r *BulletRenderer) directionChar256(velX, velY int64) rune {
	if velX == 0 && velY == 0 {
		return '*'
	}

	absX, absY := velX, velY
	if absX < 0 {
		absX = -absX
	}
	if absY < 0 {
		absY = -absY
	}

	threshold := absX / 2

	if absY < threshold {
		return '-'
	}
	if absX < threshold {
		return '|'
	}
	if (velX > 0) == (velY > 0) {
		return '\\'
	}
	return '/'
}
