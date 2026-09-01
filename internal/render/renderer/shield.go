package renderer

import (
	"math"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// ShieldStyle holds per-entity overrides for shield rendering
// Field order optimized for cache: hot fields first, cold fields last
type ShieldStyle struct {
	// Hot: accessed every cell
	Config *visual.ShieldConfig // 8 bytes - pointer to geometry/opacity
	Color  color.RGB            // 3 bytes

	// Warm: accessed for glow cells only
	GlowColor  color.RGB     // 3 bytes
	GlowPeriod time.Duration // 8 bytes

	// Cold: accessed once per entity
	Palette256 uint8   // 1 byte
	_          [1]byte // padding for alignment
	SkipX      int16   // 2 bytes (map coords fit in int16)
	SkipY      int16   // 2 bytes
}

// Total: 8 + 3 + 3 + 8 + 1 + 1 + 2 + 2 = 28 bytes (fits in half cache line)

// shieldCellFunc renders a single cell within the shield ellipse
type shieldCellFunc func(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq float64)

// ShieldPainter is a reusable shield halo renderer
type ShieldPainter struct {
	renderCell shieldCellFunc

	// Per-Paint transient state
	style            ShieldStyle
	glowActive       bool
	rotDirX, rotDirY float64
	cellDx, cellDy   float64
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
func (p *ShieldPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, style ShieldStyle) {
	p.style = style
	cfg := style.Config

	p.glowActive = style.GlowPeriod > 0
	if p.glowActive {
		period := int64(style.GlowPeriod)
		phase := ctx.GameTime.UnixNano() % period
		angle := float64(phase) / float64(period) * vmath.TwoPi
		p.rotDirX = vmath.CosF(angle)
		p.rotDirY = vmath.SinF(angle)
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

			dx := float64(mapX - centerX)
			dy := float64(mapY - centerY)
			normalizedDistSq := vmath.EllipseDistSqF(dx, dy, cfg.InvRxSq, cfg.InvRySq)

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
func shieldCellTrueColor(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq float64) {
	cfg := p.style.Config

	// Linear distance for smoother falloff
	normDist := math.Sqrt(normalizedDistSq)
	if normDist > 1.0 {
		normDist = 1.0
	}

	// Compute alpha with feather fade
	var alpha float64
	if normalizedDistSq <= visual.ShieldFeatherStart {
		// Core zone: linear falloff
		alpha = normDist * cfg.MaxOpacity
	} else {
		// Feather zone: fade from edge alpha to zero
		if visual.ShieldFeatherRange == 0.0 {
			return
		}
		edgeAlpha := math.Sqrt(visual.ShieldFeatherStart) * cfg.MaxOpacity
		fadeProgress := (normalizedDistSq - visual.ShieldFeatherStart) / visual.ShieldFeatherRange
		alpha = edgeAlpha * (1.0 - fadeProgress)
	}

	if alpha <= 0.0 {
		return
	}

	buf.Set(screenX, screenY, 0, visual.RgbBlack, p.style.Color, render.BlendScreen, alpha, terminal.AttrNone)

	// Glow overlay
	if !p.glowActive || normalizedDistSq <= visual.ShieldGlowEdgeThreshold {
		return
	}

	// Vector normalization
	cellDirX, cellDirY := vmath.Normalize2DF(p.cellDx, p.cellDy)

	dot := vmath.DotProductF(cellDirX, cellDirY, p.rotDirX, p.rotDirY)
	if dot <= 0 {
		return
	}

	edgeRange := 1.0 - visual.ShieldGlowEdgeThreshold
	if edgeRange == 0.0 {
		return
	}
	edgeFactor := (normalizedDistSq - visual.ShieldGlowEdgeThreshold) / edgeRange
	intensity := dot * edgeFactor * cfg.GlowIntensity

	buf.Set(screenX, screenY, 0, visual.RgbBlack, p.style.GlowColor, render.BlendSoftLight, intensity, terminal.AttrNone)
}

// shieldCell256 renders discrete rim for 256-color terminals
func shieldCell256(p *ShieldPainter, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq float64) {
	if normalizedDistSq < visual.Shield256Threshold {
		return
	}
	buf.SetBg256(screenX, screenY, p.style.Palette256)
}

// --- Cursor Shield Renderer ---

// emberTransitionState tracks per-entity ember-to-shield transition
type emberTransitionState struct {
	wasEmberActive  bool
	transitionStart time.Time
}

// ShieldRenderer renders active player shields with dynamic energy-based coloring
type ShieldRenderer struct {
	gameCtx *engine.GameContext
	painter *ShieldPainter

	// Per-entity ember transition tracking (keyed by entity)
	emberTransitions map[core.Entity]*emberTransitionState
}

// NewShieldRenderer creates the cursor shield system renderer
func NewShieldRenderer(gameCtx *engine.GameContext) *ShieldRenderer {
	return &ShieldRenderer{
		gameCtx:          gameCtx,
		painter:          NewShieldPainter(gameCtx.World.Resources.Config.ColorMode),
		emberTransitions: make(map[core.Entity]*emberTransitionState),
	}
}

// Render draws all active player shields
func (r *ShieldRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	shields := r.gameCtx.World.Components.Shield
	if shields.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskField)

	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	shields.Each(func(shieldEntity core.Entity, shieldComp *component.ShieldComponent) bool {
		if !shieldComp.Active {
			return true
		}

		heatComp, hasHeat := r.gameCtx.World.Components.Heat.GetPtr(shieldEntity)
		emberActive := hasHeat && heatComp.EmberActive

		// Track ember transition state (player only)
		var transitionIntensity float64
		if shieldEntity == cursorEntity {
			transition := r.getOrCreateTransition(shieldEntity)
			transitionIntensity = r.updateTransition(transition, emberActive, ctx.GameTime)
		}

		// Skip shield render when ember is active
		if emberActive {
			return true
		}

		// The ellipse is centred on its owner, so it reads that owner's cell: the
		// D-18 prediction for this instance's own cursor, the store for anything else.
		shieldPos, ok := r.gameCtx.World.CursorCell(shieldEntity)
		if !ok {
			return true
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
			if energy, ok := r.gameCtx.World.Components.Energy.GetPtr(shieldEntity); ok && energy.Current < 0 {
				style.Color = cfg.ColorAlt
				style.Palette256 = cfg.Palette256Alt
			}
			// Glow based on boost state
			if boost, ok := r.gameCtx.World.Components.Boost.GetPtr(shieldEntity); ok && boost.Active {
				style.GlowPeriod = parameter.ShieldBoostRotationDuration
			} else {
				style.GlowPeriod = 0
			}

		case component.ShieldTypeLoot:
			// GlowColor from loot visual definition
			if loot, ok := r.gameCtx.World.Components.Loot.GetPtr(shieldEntity); ok {
				if vis, exists := visual.LootVisuals[loot.Type]; exists {
					style.GlowColor = vis.GlowColor
				}
			}
		}

		r.painter.Paint(buf, ctx, shieldPos.X, shieldPos.Y, style)

		// Apply ember-to-shield transition overlay
		if transitionIntensity > 0.001 {
			r.renderTransitionOverlay(buf, ctx, shieldPos.X, shieldPos.Y, cfg, transitionIntensity)
		}
		return true
	})
}

// getOrCreateTransition returns existing or new transition state for entity
func (r *ShieldRenderer) getOrCreateTransition(entity core.Entity) *emberTransitionState {
	if t, ok := r.emberTransitions[entity]; ok {
		return t
	}
	t := &emberTransitionState{}
	r.emberTransitions[entity] = t
	return t
}

// updateTransition handles state machine and returns current overlay intensity [0,1]
func (r *ShieldRenderer) updateTransition(t *emberTransitionState, emberActive bool, now time.Time) float64 {
	// Ember reactivated - cancel any transition
	if emberActive {
		t.wasEmberActive = true
		t.transitionStart = time.Time{} // Zero value = no transition
		return 0
	}

	// Ember just ended - start transition
	if t.wasEmberActive && !emberActive {
		t.wasEmberActive = false
		t.transitionStart = now
	}

	// No active transition
	if t.transitionStart.IsZero() {
		return 0
	}

	// Calculate transition progress
	elapsed := now.Sub(t.transitionStart)
	if elapsed >= visual.EmberTransitionDuration {
		t.transitionStart = time.Time{} // Transition complete
		return 0
	}

	progress := float64(elapsed) / float64(visual.EmberTransitionDuration)
	return r.transitionEnvelope(progress)
}

// transitionEnvelope computes intensity for strobe-like fade
// Fast rise (10%) + slow fall (90%)
func (r *ShieldRenderer) transitionEnvelope(progress float64) float64 {
	rise := visual.EmberTransitionRiseRatio

	if progress < rise {
		// Fast rise: 0 → max in first 10%
		return (progress / rise) * visual.EmberTransitionMaxIntensity
	}

	// Slow fall: max → 0 in remaining 90%
	fallProgress := (progress - rise) / (1.0 - rise)
	return (1.0 - fallProgress) * visual.EmberTransitionMaxIntensity
}

// renderTransitionOverlay applies ember-colored screen blend over shield area
func (r *ShieldRenderer) renderTransitionOverlay(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, cfg *visual.ShieldConfig, intensity float64) {
	// Use ember edge color for continuity
	overlayColor := visual.RgbEmberEdgeLow

	// Bounding box matches shield visual radius
	mapStartX := max(0, centerX-cfg.VisualRadiusXInt)
	mapEndX := min(ctx.MapWidth-1, centerX+cfg.VisualRadiusXInt)
	mapStartY := max(0, centerY-cfg.VisualRadiusYInt)
	mapEndY := min(ctx.MapHeight-1, centerY+cfg.VisualRadiusYInt)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := float64(mapX - centerX)
			dy := float64(mapY - centerY)
			normDistSq := vmath.EllipseDistSqF(dx, dy, cfg.InvRxSq, cfg.InvRySq)

			// Only within shield boundary (with small margin)
			if normDistSq > visual.ShieldFeatherEnd {
				continue
			}

			// Radial falloff: stronger at edges, weaker at center
			normDist := math.Sqrt(normDistSq)
			if normDist > 1.0 {
				normDist = 1.0
			}
			radialFactor := normDist // 0 at center, 1 at edge

			// Combine intensity with radial falloff
			cellIntensity := intensity * (0.3 + 0.7*radialFactor)

			buf.Set(screenX, screenY, 0, visual.RgbBlack, overlayColor, render.BlendScreen, cellIntensity, terminal.AttrNone)
		}
	}
}
