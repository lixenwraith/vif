package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ShieldStyle configures per-invocation shield rendering parameters
type ShieldStyle struct {
	// Halo appearance
	Color      terminal.RGB
	Palette256 uint8
	MaxOpacity int64 // Q32.32 peak alpha at ellipse edge

	// Precomputed ellipse containment
	InvRxSq    int64
	InvRySq    int64
	RadiusXInt int // Integer bounding box half-width
	RadiusYInt int // Integer bounding box half-height

	// 256-color rim threshold (Q32.32, cells below this are transparent)
	Threshold256 int64

	// Game-space position to skip (-1 = disabled)
	SkipX, SkipY int

	// Rotating glow overlay (disabled if Period == 0)
	GlowColor         terminal.RGB
	GlowEdgeThreshold int64         // Q32.32 distSq below which glow is suppressed
	GlowIntensity     int64         // Q32.32 peak glow alpha
	GlowPeriod        time.Duration // Full rotation duration (0 = disabled)
}

// shieldCellFunc renders a single cell within the shield ellipse
type shieldCellFunc func(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// ShieldPainter is a reusable shield halo renderer supporting TrueColor gradient
// and 256-color rim modes with optional rotating glow overlay
type ShieldPainter struct {
	renderCell shieldCellFunc

	// Per-Paint transient state
	style            *ShieldStyle
	glowActive       bool
	rotDirX, rotDirY int64
	cellDx, cellDy   int64
}

// NewShieldPainter creates a painter dispatching to the appropriate color mode
func NewShieldPainter(colorMode terminal.ColorMode) *ShieldPainter {
	p := &ShieldPainter{}
	if colorMode == terminal.ColorMode256 {
		p.renderCell = shieldCell256
	} else {
		p.renderCell = shieldCellTrueColor
	}
	return p
}

// Paint renders a shield halo centered at (centerX, centerY) in map coordinates
// Caller must set write mask
func (p *ShieldPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, style *ShieldStyle) {
	p.style = style

	p.glowActive = style.GlowPeriod > 0
	if p.glowActive {
		period := int64(style.GlowPeriod)
		phase := ctx.GameTime.UnixNano() % period
		angle := (phase * vmath.Scale) / period
		p.rotDirX = vmath.Cos(angle)
		p.rotDirY = vmath.Sin(angle)
	}

	// Bounding box uses visual radius (includes feather zone)
	mapStartX := max(0, centerX-style.RadiusXInt)
	mapEndX := min(ctx.MapWidth-1, centerX+style.RadiusXInt)
	mapStartY := max(0, centerY-style.RadiusYInt)
	mapEndY := min(ctx.MapHeight-1, centerY+style.RadiusYInt)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			if mapX == style.SkipX && mapY == style.SkipY {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := vmath.FromInt(mapX - centerX)
			dy := vmath.FromInt(mapY - centerY)
			normalizedDistSq := vmath.EllipseDistSq(dx, dy, style.InvRxSq, style.InvRySq)

			// Extended threshold for feather zone
			if normalizedDistSq > visual.ShieldFeatherEnd {
				continue
			}

			p.cellDx = dx
			p.cellDy = dy
			p.renderCell(p, buf, screenX, screenY, normalizedDistSq)
		}
	}
}

// shieldCellTrueColor renders linear gradient with feather fade
func shieldCellTrueColor(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	s := p.style

	// Linear distance for smoother falloff
	normDist := vmath.Sqrt(normalizedDistSq)

	// Compute alpha with feather fade
	var alphaFixed int64
	if normalizedDistSq <= visual.ShieldFeatherStart {
		// Core zone: linear falloff based on distance
		alphaFixed = vmath.Mul(normDist, s.MaxOpacity)
	} else {
		// Feather zone: fade from edge alpha to zero
		edgeAlpha := vmath.Mul(vmath.Sqrt(visual.ShieldFeatherStart), s.MaxOpacity)
		fadeRange := visual.ShieldFeatherEnd - visual.ShieldFeatherStart
		fadeProgress := vmath.Div(normalizedDistSq-visual.ShieldFeatherStart, fadeRange)
		alphaFixed = vmath.Mul(edgeAlpha, vmath.Scale-fadeProgress)
	}

	if alphaFixed <= 0 {
		return
	}

	buf.Set(screenX, screenY, 0, visual.RgbBlack, s.Color, render.BlendScreen, vmath.ToFloat(alphaFixed), terminal.AttrNone)

	// Glow overlay (unchanged logic, uses distSq threshold)
	if !p.glowActive || normalizedDistSq <= s.GlowEdgeThreshold {
		return
	}

	// Use Atan2 for cell direction
	theta := vmath.Atan2(p.cellDy, p.cellDx)
	cellDirX := vmath.Cos(theta)
	cellDirY := vmath.Sin(theta)

	dot := vmath.DotProduct(cellDirX, cellDirY, p.rotDirX, p.rotDirY)
	if dot <= 0 {
		return
	}

	edgeFactor := vmath.Div(normalizedDistSq-s.GlowEdgeThreshold, vmath.Scale-s.GlowEdgeThreshold)
	intensity := vmath.Mul(vmath.Mul(dot, edgeFactor), s.GlowIntensity)
	buf.Set(screenX, screenY, 0, visual.RgbBlack, s.GlowColor, render.BlendSoftLight, vmath.ToFloat(intensity), terminal.AttrNone)
}

// shieldCell256 renders discrete rim for 256-color terminals
func shieldCell256(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	if normalizedDistSq < p.style.Threshold256 {
		return
	}
	buf.SetBg256(screenX, screenY, p.style.Palette256)
}

// --- Cursor Shield Renderer (SystemRenderer) ---

// ShieldRenderer renders active player shields with dynamic energy-based coloring
type ShieldRenderer struct {
	gameCtx *engine.GameContext
	painter *ShieldPainter
}

// NewShieldRenderer creates the cursor shield system renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	return &ShieldRenderer{
		gameCtx: gameCtx,
		painter: NewShieldPainter(gameCtx.World.Resources.Config.ColorMode),
	}
}

// Render draws all active player shields
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskField)

	shieldEntities := r.gameCtx.World.Components.Shield.GetAllEntities()
	if len(shieldEntities) == 0 {
		return
	}

	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	for _, shieldEntity := range shieldEntities {
		shieldComp, ok := r.gameCtx.World.Components.Shield.GetComponent(shieldEntity)
		if !ok || !shieldComp.Active {
			continue
		}

		// Skip shield render when ember is active
		if heatComp, ok := r.gameCtx.World.Components.Heat.GetComponent(shieldEntity); ok && heatComp.EmberActive {
			continue
		}

		shieldPos, ok := r.gameCtx.World.Positions.GetPosition(shieldEntity)
		if !ok {
			continue
		}

		cfg := &visual.ShieldConfigs[shieldComp.Type]

		// Skip position in map coords (only for cursor)
		skipX, skipY := -1, -1
		if shieldEntity == cursorEntity {
			skipX = shieldPos.X
			skipY = shieldPos.Y
		}

		// Resolve runtime-variable visuals
		color := cfg.Color
		palette := cfg.Palette256
		glowColor := cfg.GlowColor
		glowPeriod := cfg.GlowPeriod

		switch shieldComp.Type {
		case component.ShieldTypePlayer:
			// Color based on energy polarity
			if energy, ok := r.gameCtx.World.Components.Energy.GetComponent(shieldEntity); ok && energy.Current < 0 {
				color = cfg.ColorAlt
				palette = cfg.Palette256Alt
			}
			// Glow based on boost state
			if boost, ok := r.gameCtx.World.Components.Boost.GetComponent(shieldEntity); ok && boost.Active {
				glowPeriod = parameter.ShieldBoostRotationDuration
			} else {
				glowPeriod = 0
			}

		case component.ShieldTypeLoot:
			// GlowColor from loot visual definition
			if loot, ok := r.gameCtx.World.Components.Loot.GetComponent(shieldEntity); ok {
				if vis, exists := visual.LootVisuals[loot.Type]; exists {
					glowColor = vis.GlowColor
				}
			}
		}

		style := ShieldStyle{
			Color:             color,
			Palette256:        palette,
			MaxOpacity:        vmath.FromFloat(cfg.MaxOpacity),
			InvRxSq:           cfg.InvRxSq,
			InvRySq:           cfg.InvRySq,
			RadiusXInt:        cfg.VisualRadiusXInt,
			RadiusYInt:        cfg.VisualRadiusYInt,
			Threshold256:      vmath.FromFloat(0.64),
			SkipX:             skipX,
			SkipY:             skipY,
			GlowColor:         glowColor,
			GlowEdgeThreshold: vmath.FromFloat(0.36),
			GlowIntensity:     vmath.FromFloat(cfg.GlowIntensity),
			GlowPeriod:        glowPeriod,
		}

		r.painter.Paint(buf, ctx, shieldPos.X, shieldPos.Y, &style)
	}
}