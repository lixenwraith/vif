package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// === Render Data ===

// eyeCell holds resolved per-cell render data for one animation frame position
type eyeCell struct {
	Ch    rune
	FgIdx int8          // -1 = no foreground
	BgIdx int8          // -1 = transparent (preserve underlying bg)
	Attr  terminal.Attr // style bits only (Bold, Dim)
}

// eyeTypeRender holds compiled render data for one eye type
type eyeTypeRender struct {
	FgPalette  [8]terminal.RGB
	BgPalette  [3]terminal.RGB
	Fg256      uint8
	Bg256      uint8
	FrameCount int
	Frames     [visual.EyeMaxFrames][parameter.EyeHeight][parameter.EyeWidth]eyeCell
}

var eyeRenderTable [parameter.EyeTypeCount]eyeTypeRender

// === Init: Parse Visual Specs → Render Table ===

func parsePaletteIdx(b byte) int8 {
	if b >= '0' && b <= '9' {
		return int8(b - '0')
	}
	if b >= 'a' && b <= 'f' {
		return int8(b-'a') + 10
	}
	return -1
}

func parseAttr(b byte) terminal.Attr {
	switch b {
	case 'B':
		return terminal.AttrBold
	case 'D':
		return terminal.AttrDim
	default:
		return terminal.AttrNone
	}
}

func buildEyeType(spec *visual.EyeTypeVisual) eyeTypeRender {
	r := eyeTypeRender{
		FgPalette:  spec.FgPalette,
		BgPalette:  spec.BgPalette,
		Fg256:      spec.Fg256,
		Bg256:      spec.Bg256,
		FrameCount: spec.FrameCount,
	}
	for f := 0; f < spec.FrameCount; f++ {
		fs := &spec.Frames[f]
		for row := 0; row < parameter.EyeHeight; row++ {
			for col := 0; col < parameter.EyeWidth; col++ {
				cell := &r.Frames[f][row][col]
				if col < len(fs.Art[row]) {
					ch := rune(fs.Art[row][col])
					if ch != ' ' {
						cell.Ch = ch
					}
				}
				if col < len(fs.Fg[row]) {
					cell.FgIdx = parsePaletteIdx(fs.Fg[row][col])
				} else {
					cell.FgIdx = -1
				}
				if col < len(fs.Bg[row]) {
					cell.BgIdx = parsePaletteIdx(fs.Bg[row][col])
				} else {
					cell.BgIdx = -1
				}
				if col < len(fs.Attr[row]) {
					cell.Attr = parseAttr(fs.Attr[row][col])
				}
			}
		}
	}
	return r
}

func init() {
	for i := range visual.EyeTypeVisuals {
		eyeRenderTable[i] = buildEyeType(&visual.EyeTypeVisuals[i])
	}
}

// === Renderer ===

// eyeCellRenderer callback for color mode strategy (256 vs TrueColor)
type eyeCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool)

// EyeRenderer draws eye composite entities
type EyeRenderer struct {
	gameCtx    *engine.GameContext
	renderCell eyeCellRenderer
}

// NewEyeRenderer creates an eye renderer with color mode strategy
func NewEyeRenderer(gameCtx *engine.GameContext) *EyeRenderer {
	r := &EyeRenderer{gameCtx: gameCtx}
	if gameCtx.World.Resources.Config.ColorMode == terminal.ColorMode256 {
		r.renderCell = r.cell256
	} else {
		r.renderCell = r.cellTrueColor
	}
	return r
}

// Render draws all active eye entities
func (r *EyeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	headerEntities := r.gameCtx.World.Components.Eye.GetAllEntities()
	if len(headerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, headerEntity := range headerEntities {
		eyeComp, ok := r.gameCtx.World.Components.Eye.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(headerEntity)
		if !ok {
			continue
		}

		typeIdx := int(eyeComp.Type)
		if typeIdx >= parameter.EyeTypeCount {
			continue
		}

		tr := &eyeRenderTable[typeIdx]
		frameIdx := eyeComp.FrameIndex % tr.FrameCount
		frame := &tr.Frames[frameIdx]

		var flashColor terminal.RGB
		hasFlash := combatComp.RemainingHitFlash > 0
		if hasFlash {
			flashColor = calculateEyeFlashColor(combatComp.RemainingHitFlash)
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

			r.renderCell(buf, screenX, screenY, &frame[row][col], tr, flashColor, hasFlash)
		}
	}
}

// cellTrueColor renders with per-cell palette colors and attributes
func (r *EyeRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool) {
	hasCh := cell.Ch != 0 && cell.FgIdx >= 0
	hasBg := cell.BgIdx >= 0

	if !hasCh && !hasBg {
		return
	}

	if hasBg {
		buf.SetBgOnly(screenX, screenY, tr.BgPalette[cell.BgIdx])
	}

	if hasCh {
		fg := tr.FgPalette[cell.FgIdx]
		if hasFlash {
			fg = flashColor
		}
		buf.SetFgOnly(screenX, screenY, cell.Ch, fg, cell.Attr)
	}
}

// cell256 renders with simplified per-type 256-color palette
func (r *EyeRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, cell *eyeCell, tr *eyeTypeRender, flashColor terminal.RGB, hasFlash bool) {
	hasCh := cell.Ch != 0
	hasBg := cell.BgIdx >= 0

	if !hasCh && !hasBg {
		return
	}

	if hasBg {
		buf.SetBg256(screenX, screenY, tr.Bg256)
	}

	if hasCh {
		fgIdx := tr.Fg256
		if hasFlash {
			fgIdx = visual.Eye256FlashFg
		}
		buf.SetFgOnly(screenX, screenY, cell.Ch, terminal.RGB{R: fgIdx}, terminal.AttrFg256|cell.Attr)
	}
}

// calculateEyeFlashColor returns pulsed yellow flash color matching combat convention
func calculateEyeFlashColor(remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	return render.Scale(visual.RgbCombatHitFlash, intensity)
}