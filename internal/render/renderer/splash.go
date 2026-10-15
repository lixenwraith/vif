package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// SplashRenderer draws large block characters as background effect
type SplashRenderer struct {
	gameCtx *engine.GameContext
}

func NewSplashRenderer(gameCtx *engine.GameContext) *SplashRenderer {
	return &SplashRenderer{
		gameCtx: gameCtx,
	}
}

func (r *SplashRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	splashes := r.gameCtx.World.Components.Splash
	if splashes.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskTransient)

	splashes.Each(func(_ core.Entity, splash *component.SplashComponent) bool {
		length := min(max(splash.Length, 0), len(splash.Content))
		if length == 0 {
			return true
		}

		anchorX, anchorY, ok := r.resolveAnchor(splash)
		if !ok {
			return true
		}

		for i := range length {
			charX := anchorX + i*parameter.SplashCharWidth
			r.renderChar(ctx, buf, splash.Content[i], charX, anchorY, splash.Color)
		}
		return true
	})
}

// resolveAnchor returns the splash's origin in map coordinates. An anchored splash
// follows its anchor entity and has no origin once that entity has no position:
// it is leaving with its anchor, and AnchorX/Y hold nothing for it
func (r *SplashRenderer) resolveAnchor(splashComp *component.SplashComponent) (int, int, bool) {
	if splashComp.AnchorEntity == 0 {
		return splashComp.AnchorX, splashComp.AnchorY, true
	}
	anchorPos, ok := r.gameCtx.World.Positions.GetPosition(splashComp.AnchorEntity)
	if !ok {
		return 0, 0, false
	}
	return anchorPos.X + splashComp.OffsetX, anchorPos.Y + splashComp.OffsetY, true
}

// splashBitmap is one glyph of the splash font, a row per element
type splashBitmap = [parameter.SplashCharHeight]uint16

// splashGlyph returns char's glyph, and the fallback box with false where the font
// has none
func splashGlyph(char rune) (splashBitmap, bool) {
	if char < 32 || char > 126 {
		return asset.SplashFontFallback, false
	}
	return asset.SplashFont[char-32], true
}

// splashInk reports whether a glyph inks the cell at col, row; rows are MSB-first,
// bit 15 being column 0
func splashInk(bitmap splashBitmap, col, row int) bool {
	return row >= 0 && row < parameter.SplashCharHeight && col >= 0 && col < parameter.SplashCharWidth &&
		bitmap[row]&(1<<(15-col)) != 0
}

// splashInkBounds returns the rectangle a glyph inks within its cell, zero-sized
// for a blank glyph
func splashInkBounds(bitmap splashBitmap) (left, top, width, height int) {
	right, bottom := -1, -1
	left, top = parameter.SplashCharWidth, parameter.SplashCharHeight
	for row := range parameter.SplashCharHeight {
		for col := range parameter.SplashCharWidth {
			if splashInk(bitmap, col, row) {
				left, right = min(left, col), max(right, col)
				top, bottom = min(top, row), max(bottom, row)
			}
		}
	}
	if right < 0 {
		return 0, 0, 0, 0
	}
	return left, top, right - left + 1, bottom - top + 1
}

func (r *SplashRenderer) renderChar(ctx render.RenderContext, buf *render.RenderBuffer, char rune, gameX, gameY int, c color.RGB) {
	bitmap, _ := splashGlyph(char)

	for row := range parameter.SplashCharHeight {
		mapY := gameY + row

		for col := range parameter.SplashCharWidth {
			if !splashInk(bitmap, col, row) {
				continue
			}

			mapX := gameX + col

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			buf.SetBgOnly(screenX, screenY, c)
		}
	}
}
