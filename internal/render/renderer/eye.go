package renderer

import (
	"fmt"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

// eyeLooks are the eye types' looks, compiled once
var eyeLooks = func() (looks [parameter.EyeTypeCount]component.Look) {
	for i := range looks {
		look, err := visual.CompileLook(parameter.EyeWidth, parameter.EyeHeight, visual.EyeTypeVisuals[i])
		if err != nil {
			panic(fmt.Sprintf("eye type %d: %v", i, err))
		}
		looks[i] = look
	}
	return looks
}()

// EyeRenderer draws eye composite entities
type EyeRenderer struct {
	gameCtx *engine.GameContext
	paint   lookPainter
}

func NewEyeRenderer(gameCtx *engine.GameContext) *EyeRenderer {
	return &EyeRenderer{gameCtx: gameCtx, paint: newLookPainter(gameCtx.World.Resources.Config.ColorMode)}
}

// Render draws all active eye entities
func (r *EyeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	eyes := r.gameCtx.World.Components.Eye
	if eyes.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	eyes.Each(func(headerEntity core.Entity, eyeComp *component.EyeComponent) bool {
		headerComp, ok := r.gameCtx.World.Components.Header.GetPtr(headerEntity)
		if !ok {
			return true
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetPtr(headerEntity)
		if !ok {
			return true
		}

		typeIdx := int(eyeComp.Type)
		if typeIdx >= parameter.EyeTypeCount {
			return true
		}

		look := &eyeLooks[typeIdx]
		frame := look.Frames[eyeComp.FrameIndex%len(look.Frames)]

		var flash color.RGB
		hit := combatComp.RemainingHitFlash > 0
		if hit {
			flash = hitFlashColor(combatComp.RemainingHitFlash)
		}

		for _, member := range headerComp.MemberEntries {
			if member.Entity == 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(member.Entity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			row := member.OffsetY + parameter.EyeHeaderOffsetY
			col := member.OffsetX + parameter.EyeHeaderOffsetX
			if row < 0 || row >= parameter.EyeHeight || col < 0 || col >= parameter.EyeWidth {
				continue
			}

			r.paint.cell(buf, screenX, screenY, look, &frame[row*parameter.EyeWidth+col], flash, hit, hit)
		}
		return true
	})
}
