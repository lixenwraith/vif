package renderer

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// pylonColorEntry holds pre-computed color for a health ratio
type pylonColorEntry struct {
	bright terminal.RGB
	dark   terminal.RGB
}

// pylonRenderFunc defines the render strategy signature, selected at initialization
type pylonRenderFunc func(r *PylonRenderer, ctx render.RenderContext, buf *render.RenderBuffer)

// PylonRenderer draws pylon entities with health-based coloring
type PylonRenderer struct {
	gameCtx   *engine.GameContext
	colorMode terminal.ColorMode

	// Pre-computed color gradient (256 entries for health ratio 0.0-1.0)
	colorLUT [256]pylonColorEntry

	// Glow parameters
	glowColor terminal.RGB

	// Render function selected at construction
	renderFunc pylonRenderFunc
}

func NewPylonRenderer(gameCtx *engine.GameContext) *PylonRenderer {
	colorMode := gameCtx.World.Resources.Config.ColorMode

	r := &PylonRenderer{
		gameCtx:   gameCtx,
		colorMode: colorMode,
		glowColor: visual.RgbPylonGlow,
	}

	// Build color LUT
	r.buildColorLUT()

	// Select render path based on color mode
	switch colorMode {
	case terminal.ColorModeTrueColor:
		r.renderFunc = (*PylonRenderer).renderTrueColor
	case terminal.ColorMode256:
		r.renderFunc = (*PylonRenderer).render256Color
	default:
		r.renderFunc = (*PylonRenderer).renderBasicColor
	}

	return r
}

func (r *PylonRenderer) buildColorLUT() {
	for i := 0; i < 256; i++ {
		ratio := float64(i) / 255.0
		r.colorLUT[i] = r.computeColorEntry(ratio)
	}
}

func (r *PylonRenderer) computeColorEntry(healthRatio float64) pylonColorEntry {
	var bright, dark terminal.RGB

	switch {
	case healthRatio >= visual.PylonHealthThresholdDamaged:
		// Healthy: Blue
		bright = visual.RgbPylonHealthyBright
		dark = visual.RgbPylonHealthyDark

	case healthRatio >= visual.PylonHealthThresholdCritical:
		// Damaged: interpolate Blue → Green
		t := (visual.PylonHealthThresholdDamaged - healthRatio) /
			(visual.PylonHealthThresholdDamaged - visual.PylonHealthThresholdCritical)
		bright = render.Lerp(visual.RgbPylonHealthyBright, visual.RgbPylonDamagedBright, t)
		dark = render.Lerp(visual.RgbPylonHealthyDark, visual.RgbPylonDamagedDark, t)

	default:
		// Critical: interpolate Green → Red
		t := (visual.PylonHealthThresholdCritical - healthRatio) / visual.PylonHealthThresholdCritical
		bright = render.Lerp(visual.RgbPylonDamagedBright, visual.RgbPylonCriticalBright, t)
		dark = render.Lerp(visual.RgbPylonDamagedDark, visual.RgbPylonCriticalDark, t)
	}

	return pylonColorEntry{bright: bright, dark: dark}
}

func (r *PylonRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	pylonEntities := r.gameCtx.World.Components.Pylon.GetAllEntities()
	if len(pylonEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)
	r.renderFunc(r, ctx, buf)
}

func (r *PylonRenderer) renderTrueColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Pylon.GetAllEntities() {
		pylonComp, ok := r.gameCtx.World.Components.Pylon.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		// Render glow first (background layer)
		r.renderGlow(ctx, buf, &pylonComp)

		// Render members
		r.renderMembersTrueColor(ctx, buf, &pylonComp, &headerComp)
	}
}

func (r *PylonRenderer) renderGlow(ctx render.RenderContext, buf *render.RenderBuffer, pylonComp *component.PylonComponent) {
	centerX, centerY := pylonComp.SpawnX, pylonComp.SpawnY
	radiusX := float64(pylonComp.RadiusX)
	radiusY := float64(pylonComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	glowExtend := visual.PylonGlowExtendFloat
	glowOuterRadiusX := radiusX + glowExtend
	glowOuterRadiusY := radiusY + glowExtend

	// Precompute inverse squared for ellipse containment
	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	// Pulse intensity using game time
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	periodMs := int64(parameter.StormConvexGlowPeriodMs)
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	sinVal := vmath.Sin(angleFixed)
	pulse := 0.5 + 0.5*vmath.ToFloat(sinVal)

	glowIntensity := visual.PylonGlowIntensityMin +
		(visual.PylonGlowIntensityMax-visual.PylonGlowIntensityMin)*pulse

	// Bounding box
	mapStartX := max(0, centerX-int(glowOuterRadiusX)-1)
	mapEndX := min(ctx.MapWidth-1, centerX+int(glowOuterRadiusX)+1)
	mapStartY := max(0, centerY-int(glowOuterRadiusY)-1)
	mapEndY := min(ctx.MapHeight-1, centerY+int(glowOuterRadiusY)+1)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := float64(mapX - centerX)
			dy := float64(mapY - centerY)

			// Normalized ellipse distance squared: (dx/rx)² + (dy/ry)²
			normDistSq := dx*dx*invRxSq + dy*dy*invRySq
			normDist := math.Sqrt(normDistSq)

			// Skip inside body or outside glow
			if normDist <= 1.0 || normDistSq > visual.PylonGlowOuterDistSqMax {
				continue
			}

			// Falloff from edge
			alpha := 1.0 - (normDist-1.0)*visual.PylonGlowFalloffMult
			if alpha <= 0 {
				continue
			}

			factor := glowIntensity * alpha
			rVal := float64(r.glowColor.R) * factor
			gVal := float64(r.glowColor.G) * factor
			bVal := float64(r.glowColor.B) * factor

			if rVal < 1 && gVal < 1 && bVal < 1 {
				continue
			}
			if rVal > 255 {
				rVal = 255
			}
			if gVal > 255 {
				gVal = 255
			}
			if bVal > 255 {
				bVal = 255
			}

			color := terminal.RGB{R: uint8(rVal), G: uint8(gVal), B: uint8(bVal)}
			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, 1.0, terminal.AttrNone)
		}
	}
}

func (r *PylonRenderer) renderMembersTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pylonComp *component.PylonComponent,
	headerComp *component.HeaderComponent,
) {
	radiusX := float64(pylonComp.RadiusX)
	radiusY := float64(pylonComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	// Precompute inverse squared for normalized distance
	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := pylonComp.MinHP
	maxHP := pylonComp.MaxHP
	hpRange := maxHP - minHP

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(member.Entity)
		if !ok || combatComp.HitPoints <= 0 {
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

		// Normalized ellipse distance for HP calculation
		dx := float64(member.OffsetX)
		dy := float64(member.OffsetY)
		normDistSq := dx*dx*invRxSq + dy*dy*invRySq
		normDist := math.Sqrt(normDistSq)

		var initialHP int
		if hpRange > 0 {
			if normDist > 1.0 {
				normDist = 1.0
			}
			initialHP = maxHP - int(float64(hpRange)*normDist)
		} else {
			initialHP = maxHP
		}
		if initialHP < minHP {
			initialHP = minHP
		}
		if initialHP <= 0 {
			initialHP = 1
		}

		// Health ratio for color
		healthRatio := float64(combatComp.HitPoints) / float64(initialHP)
		if healthRatio > 1.0 {
			healthRatio = 1.0
		}
		if healthRatio < 0.0 {
			healthRatio = 0.0
		}

		// Brightness factor based on normalized position (center=1.0, edge=0.6)
		positionBrightness := 1.0 - 0.4*normDist
		if positionBrightness < 0.6 {
			positionBrightness = 0.6
		}

		// Look up color from LUT
		lutIdx := int(healthRatio * 255)
		if lutIdx > 255 {
			lutIdx = 255
		}
		entry := r.colorLUT[lutIdx]

		// Interpolate between bright and dark based on position
		color := render.Lerp(entry.dark, entry.bright, positionBrightness)
		buf.SetBgOnly(screenX, screenY, color)
	}
}

func (r *PylonRenderer) render256Color(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Pylon.GetAllEntities() {
		pylonComp, ok := r.gameCtx.World.Components.Pylon.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		r.renderMembers256Color(ctx, buf, &pylonComp, &headerComp)
	}
}

func (r *PylonRenderer) renderMembers256Color(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pylonComp *component.PylonComponent,
	headerComp *component.HeaderComponent,
) {
	radiusX := float64(pylonComp.RadiusX)
	radiusY := float64(pylonComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := pylonComp.MinHP
	maxHP := pylonComp.MaxHP
	hpRange := maxHP - minHP

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(member.Entity)
		if !ok || combatComp.HitPoints <= 0 {
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

		dx := float64(member.OffsetX)
		dy := float64(member.OffsetY)
		normDistSq := dx*dx*invRxSq + dy*dy*invRySq
		normDist := math.Sqrt(normDistSq)

		var initialHP int
		if hpRange > 0 {
			if normDist > 1.0 {
				normDist = 1.0
			}
			initialHP = maxHP - int(float64(hpRange)*normDist)
		} else {
			initialHP = maxHP
		}
		if initialHP < minHP {
			initialHP = minHP
		}
		if initialHP <= 0 {
			initialHP = 1
		}

		healthRatio := float64(combatComp.HitPoints) / float64(initialHP)
		if healthRatio > 1.0 {
			healthRatio = 1.0
		}

		// Select palette index based on health zone
		var paletteIdx uint8
		switch {
		case healthRatio >= visual.PylonHealthThresholdDamaged:
			paletteIdx = visual.Pylon256Healthy
		case healthRatio >= visual.PylonHealthThresholdCritical:
			paletteIdx = visual.Pylon256Damaged
		default:
			paletteIdx = visual.Pylon256Critical
		}

		buf.SetBg256(screenX, screenY, paletteIdx)
	}
}

func (r *PylonRenderer) renderBasicColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Pylon.GetAllEntities() {
		pylonComp, ok := r.gameCtx.World.Components.Pylon.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		r.renderMembersBasicColor(ctx, buf, &pylonComp, &headerComp)
	}
}

func (r *PylonRenderer) renderMembersBasicColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	pylonComp *component.PylonComponent,
	headerComp *component.HeaderComponent,
) {
	radiusX := float64(pylonComp.RadiusX)
	radiusY := float64(pylonComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := pylonComp.MinHP
	maxHP := pylonComp.MaxHP
	hpRange := maxHP - minHP

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(member.Entity)
		if !ok || combatComp.HitPoints <= 0 {
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

		dx := float64(member.OffsetX)
		dy := float64(member.OffsetY)
		normDistSq := dx*dx*invRxSq + dy*dy*invRySq
		normDist := math.Sqrt(normDistSq)

		var initialHP int
		if hpRange > 0 {
			if normDist > 1.0 {
				normDist = 1.0
			}
			initialHP = maxHP - int(float64(hpRange)*normDist)
		} else {
			initialHP = maxHP
		}
		if initialHP < minHP {
			initialHP = minHP
		}
		if initialHP <= 0 {
			initialHP = 1
		}

		healthRatio := float64(combatComp.HitPoints) / float64(initialHP)
		if healthRatio > 1.0 {
			healthRatio = 1.0
		}

		var colorIdx uint8
		switch {
		case healthRatio >= visual.PylonHealthThresholdDamaged:
			colorIdx = visual.PylonBasicHealthy
		case healthRatio >= visual.PylonHealthThresholdCritical:
			colorIdx = visual.PylonBasicDamaged
		default:
			colorIdx = visual.PylonBasicCritical
		}

		buf.SetBg256(screenX, screenY, colorIdx)
	}
}