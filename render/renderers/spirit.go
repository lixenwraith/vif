package renderers

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// SpiritRenderer draws converging spirit entities with blinking effect
type SpiritRenderer struct {
	gameCtx     *engine.GameContext
	spiritStore *engine.Store[component.SpiritComponent]
}

func NewSpiritRenderer(gameCtx *engine.GameContext) *SpiritRenderer {
	return &SpiritRenderer{
		gameCtx:     gameCtx,
		spiritStore: engine.GetStore[component.SpiritComponent](gameCtx.World),
	}
}

func (r *SpiritRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	entities := r.spiritStore.All()
	if len(entities) == 0 {
		return
	}

	buf.SetWriteMask(constant.MaskTransient)

	// Calculate blink phase from frame number
	// SpiritBlinkHz cycles per second, ~60 frames/sec
	blinkPhase := (ctx.FrameNumber * int64(constant.SpiritBlinkHz) / 60) % 2

	for _, entity := range entities {
		spirit, ok := r.spiritStore.Get(entity)
		if !ok {
			continue
		}

		// Calculate interpolated position
		currentX := vmath.Lerp(spirit.StartX, spirit.TargetX, spirit.Progress)
		currentY := vmath.Lerp(spirit.StartY, spirit.TargetY, spirit.Progress)

		screenX := ctx.GameX + vmath.ToInt(currentX)
		screenY := ctx.GameY + vmath.ToInt(currentY)

		// Bounds check
		if screenX < ctx.GameX || screenX >= ctx.Width ||
			screenY < ctx.GameY || screenY >= ctx.GameY+ctx.GameHeight {
			continue
		}

		// Select color based on blink phase
		var color render.RGB
		if blinkPhase == 0 {
			color = render.RGB(spirit.BaseColor)
		} else {
			color = render.RGB(spirit.BlinkColor)
		}

		// Intensity increases as spirit approaches target (more visible near convergence)
		progress := vmath.ToFloat(spirit.Progress)
		intensity := 0.5 + (progress * 0.5) // 0.5 to 1.0

		scaledColor := render.Scale(color, intensity)

		// Additive blend for glow effect
		buf.Set(screenX, screenY, spirit.Rune, scaledColor, render.RGBBlack, render.BlendAddFg, 1.0, terminal.AttrNone)
	}
}