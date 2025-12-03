package renderers

import (
	"math"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx *engine.GameContext
}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	return &ShieldRenderer{gameCtx: gameCtx}
}

// Render draws all active shields with quadratic falloff gradient
func (s *ShieldRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	shields := world.Shields.All()

	for _, entity := range shields {
		shield, okS := world.Shields.Get(entity)
		pos, okP := world.Positions.Get(entity)

		if !okS || !okP {
			continue
		}

		// Shield only renders if Sources != 0 AND Energy > 0
		if shield.Sources == 0 || ctx.Energy <= 0 {
			continue
		}

		// Resolve shield color
		shieldRGB := s.resolveShieldColor(shield)

		// Bounding box
		startX := int(float64(pos.X) - shield.RadiusX)
		endX := int(float64(pos.X) + shield.RadiusX)
		startY := int(float64(pos.Y) - shield.RadiusY)
		endY := int(float64(pos.Y) + shield.RadiusY)

		// Clamp to screen bounds
		if startX < 0 {
			startX = 0
		}
		if endX >= ctx.GameWidth {
			endX = ctx.GameWidth - 1
		}
		if startY < 0 {
			startY = 0
		}
		if endY >= ctx.GameHeight {
			endY = ctx.GameHeight - 1
		}

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position - cursor is absolute top
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				dx := float64(x - pos.X)
				dy := float64(y - pos.Y)

				// Elliptical distance: (dx/rx)² + (dy/ry)²
				normalizedDistSq := (dx*dx)/(shield.RadiusX*shield.RadiusX) + (dy*dy)/(shield.RadiusY*shield.RadiusY)

				if normalizedDistSq > 1.0 {
					continue // Outside shield
				}

				dist := math.Sqrt(normalizedDistSq)

				// Quadratic soft falloff: (1-d)² * MaxOpacity
				falloff := (1.0 - dist) * (1.0 - dist)
				alpha := falloff * shield.MaxOpacity

				// SoftLight blend for gentler falloff
				buf.Set(screenX, screenY, 0, render.RGBBlack, shieldRGB, render.BlendSoftLight, alpha, terminal.AttrNone)
			}
		}
	}
}

// resolveShieldColor determines the shield color from override or GameState
func (s *ShieldRenderer) resolveShieldColor(shield components.ShieldComponent) render.RGB {
	// Check override first
	if shield.OverrideColor != components.ColorNone {
		return s.colorClassToRGB(shield.OverrideColor)
	}

	// Derive from GameState
	seqType := s.gameCtx.State.GetLastTypedSeqType()
	seqLevel := s.gameCtx.State.GetLastTypedSeqLevel()

	return s.getColorFromSequence(seqType, seqLevel)
}

// getColorFromSequence maps sequence type/level to RGB
func (s *ShieldRenderer) getColorFromSequence(seqType, seqLevel int32) render.RGB {
	// 0=None, 1=Blue, 2=Green
	switch seqType {
	case 1: // Blue
		switch seqLevel {
		case 0:
			return render.RgbSequenceBlueDark
		case 1:
			return render.RgbSequenceBlueNormal
		case 2:
			return render.RgbSequenceBlueBright
		}
	case 2: // Green
		switch seqLevel {
		case 0:
			return render.RgbSequenceGreenDark
		case 1:
			return render.RgbSequenceGreenNormal
		case 2:
			return render.RgbSequenceGreenBright
		}
	}
	// Default: neutral gray when no character typed yet
	return render.RGB{R: 128, G: 128, B: 128}
}

// colorClassToRGB maps ColorClass overrides to RGB (for future super modes)
func (s *ShieldRenderer) colorClassToRGB(color components.ColorClass) render.RGB {
	switch color {
	case components.ColorShield:
		return render.RgbShieldBase
	case components.ColorNugget:
		return render.RgbNuggetOrange
	default:
		return render.RgbShieldBase
	}
}