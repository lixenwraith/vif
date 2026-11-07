package renderer

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// splashLarge is the face a button's key is drawn in
var splashLarge = &asset.SplashFaces[component.SplashLarge]

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
	console := r.gameCtx.World.Resources.Config.TextConsole

	splashes.Each(func(_ core.Entity, splash *component.SplashComponent) bool {
		length := min(max(splash.Length, 0), len(splash.Content))
		if length == 0 || int(splash.Font) >= len(asset.SplashFaces) {
			return true
		}

		anchorX, anchorY, ok := r.resolveAnchor(splash)
		if !ok {
			return true
		}

		face := &asset.SplashFaces[splash.Font]
		pack := face.Packed(console)
		for i := range length {
			r.renderChar(ctx, buf, face, pack, splash.Content[i], anchorX+i*face.Width, anchorY, splash.Color)
		}
		return true
	})
}

// resolveAnchor returns the splash's origin in map coordinates, its offset from
// AnchorX/Y or from its anchor entity. An anchored splash follows its anchor and has
// no origin once that entity has no position: it is leaving with its anchor
func (r *SplashRenderer) resolveAnchor(splashComp *component.SplashComponent) (int, int, bool) {
	if splashComp.AnchorEntity == 0 {
		return splashComp.AnchorX + splashComp.OffsetX, splashComp.AnchorY + splashComp.OffsetY, true
	}
	anchorPos, ok := r.gameCtx.World.Positions.GetPosition(splashComp.AnchorEntity)
	if !ok {
		return 0, 0, false
	}
	return anchorPos.X + splashComp.OffsetX, anchorPos.Y + splashComp.OffsetY, true
}

// renderChar draws char's glyph from the cell at gameX, gameY, pack pixel rows a
// cell: a cell inked whole as its background, under whatever glyph it shows, and one
// inked in one of its two rows as a half block in the colour over the background it had
func (r *SplashRenderer) renderChar(ctx render.RenderContext, buf *render.RenderBuffer, face *asset.Face, pack int, char rune, gameX, gameY int, c color.RGB) {
	bitmap, _ := face.Glyph(char)
	_, rows := face.Cells(pack)

	for row := range rows {
		mapY := gameY + row

		for col := range face.Width {
			top := face.Ink(bitmap, col, row*pack)
			bottom := pack > 1 && face.Ink(bitmap, col, row*pack+1)
			if !top && !bottom {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(gameX+col, mapY)
			if !visible {
				continue
			}

			switch {
			case top && (bottom || pack == 1):
				buf.SetBgOnly(screenX, screenY, c)
			case top:
				buf.SetWithBg(screenX, screenY, '▀', c, buf.BackgroundAt(screenX, screenY, visual.RgbBackground))
			default:
				buf.SetWithBg(screenX, screenY, '▄', c, buf.BackgroundAt(screenX, screenY, visual.RgbBackground))
			}
		}
	}
}
