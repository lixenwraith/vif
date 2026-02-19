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

// ShieldStyle holds per-entity overrides for shield rendering
// Field order optimized for cache: hot fields first, cold fields last
type ShieldStyle struct {
	// Hot: accessed every cell
	Config *visual.ShieldConfig // 8 bytes - pointer to geometry/opacity
	Color  terminal.RGB         // 3 bytes

	// Warm: accessed for glow cells only
	GlowColor  terminal.RGB  // 3 bytes
	GlowPeriod time.Duration // 8 bytes

	// Cold: accessed once per entity
	Palette256 uint8   // 1 byte
	_          [1]byte // padding for alignment
	SkipX      int16   // 2 bytes (map coords fit in int16)
	SkipY      int16   // 2 bytes
}

// Total: 8 + 3 + 3 + 8 + 1 + 1 + 2 + 2 = 28 bytes (fits in half cache line)

// // ShieldStyle configures per-invocation shield rendering parameters
// type ShieldStyle struct {
// 	// References ShieldConfig for precomputed geometry and defaults
// 	Config *visual.ShieldConfig
//
// 	// Per-entity overrides (only fields that vary at runtime)
// 	Color      terminal.RGB
// 	Palette256 uint8
// 	GlowColor  terminal.RGB
// 	GlowPeriod time.Duration
//
// 	// Skip position in map coords (-1 = disabled)
// 	SkipX, SkipY int
//
// 	// Precomputed ellipse containment
// 	InvRxSq    int64
// 	InvRySq    int64
// 	RadiusXInt int // Integer bounding box half-width
// 	RadiusYInt int // Integer bounding box half-height
//
// 	// 256-color rim threshold (Q32.32, cells below this are transparent)
// 	Threshold256 int64
//
// 	// Rotating glow overlay (disabled if Period == 0)
// 	GlowEdgeThreshold int64 // Q32.32 distSq below which glow is suppressed
// 	GlowIntensity     int64 // Q32.32 peak glow alpha
// }

// shieldCellFunc renders a single cell within the shield ellipse
type shieldCellFunc func(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64)

// ShieldPainter is a reusable shield halo renderer
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
	cfg := style.Config

	p.glowActive = style.GlowPeriod > 0
	if p.glowActive {
		period := int64(style.GlowPeriod)
		phase := ctx.GameTime.UnixNano() % period
		angle := (phase * vmath.Scale) / period
		p.rotDirX = vmath.Cos(angle)
		p.rotDirY = vmath.Sin(angle)
	}

	// Bounding box uses visual radius from config (includes feather zone)
	mapStartX := max(0, centerX-cfg.VisualRadiusXInt)
	mapEndX := min(ctx.MapWidth-1, centerX+cfg.VisualRadiusXInt)
	mapStartY := max(0, centerY-cfg.VisualRadiusYInt)
	mapEndY := min(ctx.MapHeight-1, centerY+cfg.VisualRadiusYInt)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			if int16(mapX) == style.SkipX && int16(mapY) == style.SkipY {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := vmath.FromInt(mapX - centerX)
			dy := vmath.FromInt(mapY - centerY)
			normalizedDistSq := vmath.EllipseDistSq(dx, dy, cfg.InvRxSq, cfg.InvRySq)

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
	cfg := p.style.Config

	// Linear distance for smoother falloff
	normDist := vmath.Sqrt(normalizedDistSq)
	if normDist > vmath.Scale {
		normDist = vmath.Scale
	}

	// Compute alpha with feather fade
	var alphaFixed int64
	if normalizedDistSq <= visual.ShieldFeatherStart {
		// Core zone: linear falloff
		alphaFixed = vmath.Mul(normDist, cfg.MaxOpacityQ32)
	} else {
		// Feather zone: fade from edge alpha to zero
		edgeAlpha := vmath.Mul(vmath.Sqrt(visual.ShieldFeatherStart), cfg.MaxOpacityQ32)
		fadeProgress := vmath.Div(normalizedDistSq-visual.ShieldFeatherStart, visual.ShieldFeatherRange)
		alphaFixed = vmath.Mul(edgeAlpha, vmath.Scale-fadeProgress)
	}

	if alphaFixed <= 0 {
		return
	}

	buf.Set(screenX, screenY, 0, visual.RgbBlack, p.style.Color, render.BlendScreen, vmath.ToFloat(alphaFixed), terminal.AttrNone)

	// Glow overlay
	if !p.glowActive || normalizedDistSq <= visual.ShieldGlowEdgeThreshold {
		return
	}

	theta := vmath.Atan2(p.cellDy, p.cellDx)
	cellDirX := vmath.Cos(theta)
	cellDirY := vmath.Sin(theta)

	dot := vmath.DotProduct(cellDirX, cellDirY, p.rotDirX, p.rotDirY)
	if dot <= 0 {
		return
	}

	edgeFactor := vmath.Div(normalizedDistSq-visual.ShieldGlowEdgeThreshold, vmath.Scale-visual.ShieldGlowEdgeThreshold)
	intensity := vmath.Mul(vmath.Mul(dot, edgeFactor), cfg.GlowIntensityQ32)
	buf.Set(screenX, screenY, 0, visual.RgbBlack, p.style.GlowColor, render.BlendSoftLight, vmath.ToFloat(intensity), terminal.AttrNone)
}

// shieldCell256 renders discrete rim for 256-color terminals
func shieldCell256(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq int64) {
	if normalizedDistSq < visual.Shield256Threshold {
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

		// Build minimal per-entity style
		style := ShieldStyle{
			Config:     cfg,
			Color:      cfg.Color,
			Palette256: cfg.Palette256,
			GlowColor:  cfg.GlowColor,
			GlowPeriod: cfg.GlowPeriod,
			SkipX:      -1,
			SkipY:      -1,
		}

		// Per-entity overrides
		if shieldEntity == cursorEntity {
			style.SkipX = int16(shieldPos.X)
			style.SkipY = int16(shieldPos.Y)
		}

		switch shieldComp.Type {
		case component.ShieldTypePlayer:
			// Color based on energy polarity
			if energy, ok := r.gameCtx.World.Components.Energy.GetComponent(shieldEntity); ok && energy.Current < 0 {
				style.Color = cfg.ColorAlt
				style.Palette256 = cfg.Palette256Alt
			}
			// Glow based on boost state
			if boost, ok := r.gameCtx.World.Components.Boost.GetComponent(shieldEntity); ok && boost.Active {
				style.GlowPeriod = parameter.ShieldBoostRotationDuration
			} else {
				style.GlowPeriod = 0
			}

		case component.ShieldTypeLoot:
			// GlowColor from loot visual definition
			if loot, ok := r.gameCtx.World.Components.Loot.GetComponent(shieldEntity); ok {
				if vis, exists := visual.LootVisuals[loot.Type]; exists {
					style.GlowColor = vis.GlowColor
				}
			}
		}

		r.painter.Paint(buf, ctx, shieldPos.X, shieldPos.Y, &style)
	}
}