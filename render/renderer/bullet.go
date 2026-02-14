package renderer

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

type bulletRenderFunc func(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pos *component.PositionComponent,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
)

type BulletRenderer struct {
	gameCtx      *engine.GameContext
	renderBullet bulletRenderFunc
}

func NewBulletRenderer(gameCtx *engine.GameContext) *BulletRenderer {
	r := &BulletRenderer{gameCtx: gameCtx}
	if gameCtx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderBullet = r.renderBullet256
	} else {
		r.renderBullet = r.renderBulletTrueColor
	}
	return r
}

func (r *BulletRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.gameCtx.World.Components.Bullet.GetAllEntities()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	for _, e := range entities {
		bullet, ok := r.gameCtx.World.Components.Bullet.GetComponent(e)
		if !ok {
			continue
		}
		pos, ok := r.gameCtx.World.Positions.GetPosition(e)
		if !ok {
			continue
		}
		kinetic, ok := r.gameCtx.World.Components.Kinetic.GetComponent(e)
		if !ok {
			continue
		}
		r.renderBullet(ctx, buf, &pos, &kinetic, &bullet)
	}
}

func (r *BulletRenderer) renderBulletTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pos *component.PositionComponent,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
) {
	screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
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
	color := visual.RgbBulletStormRed
	if lifetimeRatio > 0.5 {
		t := (lifetimeRatio - 0.5) / 0.5
		color = render.Lerp(visual.RgbBulletStormRed, visual.RgbBulletStormRedDim, t)
	}

	char := r.directionChar(kinetic.VelX, kinetic.VelY)
	buf.Set(screenX, screenY, char, color, visual.RgbBlack, render.BlendAddFg, alpha, terminal.AttrBold)
}

func (r *BulletRenderer) renderBullet256(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pos *component.PositionComponent,
	kinetic *component.KineticComponent,
	bullet *component.BulletComponent,
) {
	screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
	if !visible {
		return
	}

	// Binary visibility: hide in final 20%
	lifetimeRatio := float64(bullet.Lifetime) / float64(bullet.MaxLifetime)
	if lifetimeRatio > 0.8 {
		return
	}

	char := r.directionChar256(kinetic.VelX, kinetic.VelY)
	buf.SetFgOnly(screenX, screenY, char, terminal.RGB{R: visual.Bullet256StormRed}, terminal.AttrFg256|terminal.AttrBold)
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