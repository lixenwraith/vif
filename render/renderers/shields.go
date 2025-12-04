package renderers

import (
	"math"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// shieldCellRenderer is the callback type for per-cell shield rendering
type shieldCellRenderer func(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64)

// ShieldRenderer renders active shields with dynamic color from GameState
type ShieldRenderer struct {
	gameCtx *engine.GameContext

	// Cached cell renderers for each mode
	renderCellTrueColor shieldCellRenderer
	renderCell256       shieldCellRenderer
}

// NewShieldRenderer creates a new shield renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	s := &ShieldRenderer{gameCtx: gameCtx}

	// Pre-bind cell renderers to avoid allocation in render loop
	s.renderCellTrueColor = s.cellTrueColor
	s.renderCell256 = s.cell256

	return s
}

// Render draws all active shields with quadratic falloff gradient
func (s *ShieldRenderer) Render(ctx render.RenderContext, world *engine.World, buf *render.RenderBuffer) {
	shields := world.Shields.All()
	if len(shields) == 0 {
		return
	}

	// Select cell renderer once per frame
	colorMode := s.gameCtx.Terminal.ColorMode()
	var renderCell shieldCellRenderer
	if colorMode == terminal.ColorMode256 {
		renderCell = s.renderCell256
	} else {
		renderCell = s.renderCellTrueColor
	}

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

		// Precompute inverse radii squared for ellipse calculation
		invRxSq := 1.0 / (shield.RadiusX * shield.RadiusX)
		invRySq := 1.0 / (shield.RadiusY * shield.RadiusY)

		for y := startY; y <= endY; y++ {
			for x := startX; x <= endX; x++ {
				// Skip cursor position
				if x == ctx.CursorX && y == ctx.CursorY {
					continue
				}

				dx := float64(x - pos.X)
				dy := float64(y - pos.Y)

				// Elliptical normalized distance squared
				normalizedDistSq := dx*dx*invRxSq + dy*dy*invRySq
				if normalizedDistSq > 1.0 {
					continue
				}

				dist := math.Sqrt(normalizedDistSq)
				screenX := ctx.GameX + x
				screenY := ctx.GameY + y

				renderCell(buf, screenX, screenY, dist, shieldRGB, shield.MaxOpacity)
			}
		}
	}
}

// cellTrueColor renders a single shield cell with smooth gradient (TrueColor mode)
func (s *ShieldRenderer) cellTrueColor(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
	// Quadratic soft falloff: (1-d)² * MaxOpacity
	falloff := (1.0 - dist) * (1.0 - dist)
	alpha := falloff * maxOpacity

	// BlendScreen: Adds light, visible on black, brightens over content
	buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, alpha, terminal.AttrNone)
}

// cell256 renders a single shield cell with discrete zones (256-color mode)
func (s *ShieldRenderer) cell256(buf *render.RenderBuffer, screenX, screenY int, dist float64, color render.RGB, maxOpacity float64) {
	// Palette-safe grayscale colors
	// Index 238 = RGB(68,68,68) - dark gray
	// Index 250 = RGB(188,188,188) - light gray
	// Index 253 = RGB(218,218,218) - near-white for edge

	switch {
	case dist > 0.9:
		// Edge zone: Attempt tinted glow via low-alpha screen blend
		// If shield color is too dark, this still produces visible edge
		buf.Set(screenX, screenY, 0, render.RGBBlack, color, render.BlendScreen, 0.4, terminal.AttrNone)

	case dist > 0.6:
		// Outer zone: Light gray, solid replace
		buf.Set(screenX, screenY, 0, render.RGBBlack, render.RGB{R: 188, G: 188, B: 188}, render.BlendReplace, 1.0, terminal.AttrNone)

	default:
		// Inner zone: Dark gray, solid replace
		buf.Set(screenX, screenY, 0, render.RGBBlack, render.RGB{R: 68, G: 68, B: 68}, render.BlendReplace, 1.0, terminal.AttrNone)
	}
}

// resolveShieldColor determines the shield color from override or GameState
func (s *ShieldRenderer) resolveShieldColor(shield components.ShieldComponent) render.RGB {
	if shield.OverrideColor != components.ColorNone {
		return s.colorClassToRGB(shield.OverrideColor)
	}

	seqType := s.gameCtx.State.GetLastTypedSeqType()
	seqLevel := s.gameCtx.State.GetLastTypedSeqLevel()

	return s.getColorFromSequence(seqType, seqLevel)
}

// getColorFromSequence maps sequence type/level to RGB
func (s *ShieldRenderer) getColorFromSequence(seqType, seqLevel int32) render.RGB {
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

// colorClassToRGB maps ColorClass overrides to RGB
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
