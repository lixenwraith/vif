package renderer

import (
	"math"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// towerColorEntry holds pre-computed color pair for a health ratio
type towerColorEntry struct {
	bright terminal.RGB
	dark   terminal.RGB
}

// towerRenderFunc defines the render strategy signature, selected at initialization
type towerRenderFunc func(r *TowerRenderer, ctx render.RenderContext, buf *render.RenderBuffer)

// TowerRenderer draws tower entities with health-based coloring and target-aware glow
type TowerRenderer struct {
	gameCtx   *engine.GameContext
	colorMode terminal.ColorMode

	// Pre-computed color gradient per visual type (256 entries for health ratio 0.0-1.0)
	colorLUTs [component.TowerTypeCount][256]towerColorEntry

	// Render function selected at construction
	renderFunc towerRenderFunc
}

func NewTowerRenderer(gameCtx *engine.GameContext) *TowerRenderer {
	colorMode := gameCtx.World.Resources.Config.ColorMode

	r := &TowerRenderer{
		gameCtx:   gameCtx,
		colorMode: colorMode,
	}

	r.buildColorLUTs()

	switch colorMode {
	case terminal.ColorModeTrueColor:
		r.renderFunc = (*TowerRenderer).renderTrueColor
	case terminal.ColorMode256:
		r.renderFunc = (*TowerRenderer).render256Color
	default:
		r.renderFunc = (*TowerRenderer).renderBasicColor
	}

	return r
}

func (r *TowerRenderer) buildColorLUTs() {
	for t := 0; t < int(component.TowerTypeCount); t++ {
		for i := 0; i < 256; i++ {
			ratio := float64(i) / 255.0
			r.colorLUTs[t][i] = computeTowerColorEntry(t, ratio)
		}
	}
}

func computeTowerColorEntry(visualType int, healthRatio float64) towerColorEntry {
	tc := &visual.TowerTypes[visualType]
	var bright, dark terminal.RGB

	switch {
	case healthRatio >= visual.TowerHealthThresholdDamaged:
		bright = tc.HealthyBright
		dark = tc.HealthyDark

	case healthRatio >= visual.TowerHealthThresholdCritical:
		t := (visual.TowerHealthThresholdDamaged - healthRatio) /
			(visual.TowerHealthThresholdDamaged - visual.TowerHealthThresholdCritical)
		bright = render.Lerp(tc.HealthyBright, tc.DamagedBright, t)
		dark = render.Lerp(tc.HealthyDark, tc.DamagedDark, t)

	default:
		t := (visual.TowerHealthThresholdCritical - healthRatio) / visual.TowerHealthThresholdCritical
		bright = render.Lerp(tc.DamagedBright, tc.CriticalBright, t)
		dark = render.Lerp(tc.DamagedDark, tc.CriticalDark, t)
	}

	return towerColorEntry{bright: bright, dark: dark}
}

func (r *TowerRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	towerEntities := r.gameCtx.World.Components.Tower.GetAllEntities()
	if len(towerEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)
	r.renderFunc(r, ctx, buf)
}

// isActiveTarget returns true if any target group references this tower header entity
func (r *TowerRenderer) isActiveTarget(headerEntity core.Entity) bool {
	for i := range r.gameCtx.World.Resources.Target.Groups {
		g := &r.gameCtx.World.Resources.Target.Groups[i]
		if g.Valid && g.Type == component.TargetEntity && g.Entity == headerEntity {
			return true
		}
	}
	return false
}

// clampVisualType returns a valid visual type index, defaulting to 0
func clampVisualType(vt component.TowerType) int {
	if vt >= component.TowerTypeCount {
		return 0
	}
	return int(vt)
}

// === TrueColor ===

func (r *TowerRenderer) renderTrueColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Tower.GetAllEntities() {
		towerComp, ok := r.gameCtx.World.Components.Tower.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		visualType := clampVisualType(towerComp.Type)
		active := r.isActiveTarget(headerEntity)

		// Glow parameters: active target uses brighter, faster pulse
		var glowColor terminal.RGB
		var intensityMin, intensityMax float64
		var periodMs int64

		if active {
			glowColor = visual.RgbTowerActiveGlow
			intensityMin = visual.TowerActiveGlowIntensityMin
			intensityMax = visual.TowerActiveGlowIntensityMax
			periodMs = visual.TowerActiveGlowPeriodMs
		} else {
			glowColor = visual.TowerTypes[visualType].GlowColor
			intensityMin = visual.TowerGlowIntensityMin
			intensityMax = visual.TowerGlowIntensityMax
			periodMs = visual.TowerGlowPeriodMs
		}

		r.renderGlow(ctx, buf, &towerComp, glowColor, intensityMin, intensityMax, periodMs)
		r.renderMembersTrueColor(ctx, buf, &towerComp, &headerComp, visualType)
	}
}

func (r *TowerRenderer) renderGlow(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	towerComp *component.TowerComponent,
	glowColor terminal.RGB,
	intensityMin, intensityMax float64,
	periodMs int64,
) {
	centerX, centerY := towerComp.SpawnX, towerComp.SpawnY
	radiusX := float64(towerComp.RadiusX)
	radiusY := float64(towerComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	glowOuterRadiusX := radiusX + visual.TowerGlowExtendFloat
	glowOuterRadiusY := radiusY + visual.TowerGlowExtendFloat

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	// Pulse intensity using game time
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	sinVal := vmath.Sin(angleFixed)
	pulse := 0.5 + 0.5*vmath.ToFloat(sinVal)

	glowIntensity := intensityMin + (intensityMax-intensityMin)*pulse

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

			normDistSq := dx*dx*invRxSq + dy*dy*invRySq
			normDist := math.Sqrt(normDistSq)

			// Skip inside body or outside glow
			if normDist <= 1.0 || normDistSq > visual.TowerGlowOuterDistSqMax {
				continue
			}

			alpha := 1.0 - (normDist-1.0)*visual.TowerGlowFalloffMult
			if alpha <= 0 {
				continue
			}

			factor := glowIntensity * alpha
			rVal := float64(glowColor.R) * factor
			gVal := float64(glowColor.G) * factor
			bVal := float64(glowColor.B) * factor

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

func (r *TowerRenderer) renderMembersTrueColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	towerComp *component.TowerComponent,
	headerComp *component.HeaderComponent,
	visualType int,
) {
	radiusX := float64(towerComp.RadiusX)
	radiusY := float64(towerComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := towerComp.MinHP
	maxHP := towerComp.MaxHP
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
		if healthRatio < 0.0 {
			healthRatio = 0.0
		}

		positionBrightness := 1.0 - visual.TowerEdgeDimFactor*normDist
		if positionBrightness < visual.TowerEdgeBrightnessMin {
			positionBrightness = visual.TowerEdgeBrightnessMin
		}

		lutIdx := int(healthRatio * 255)
		if lutIdx > 255 {
			lutIdx = 255
		}
		entry := r.colorLUTs[visualType][lutIdx]

		color := render.Lerp(entry.dark, entry.bright, positionBrightness)
		buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendMax, 1.0, terminal.AttrNone)
	}
}

// === 256-Color ===

func (r *TowerRenderer) render256Color(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Tower.GetAllEntities() {
		towerComp, ok := r.gameCtx.World.Components.Tower.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		visualType := clampVisualType(towerComp.Type)
		r.renderMembers256Color(ctx, buf, &towerComp, &headerComp, visualType)
	}
}

func (r *TowerRenderer) renderMembers256Color(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	towerComp *component.TowerComponent,
	headerComp *component.HeaderComponent,
	visualType int,
) {
	radiusX := float64(towerComp.RadiusX)
	radiusY := float64(towerComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := towerComp.MinHP
	maxHP := towerComp.MaxHP
	hpRange := maxHP - minHP

	tc := &visual.TowerTypes[visualType]

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

		var paletteIdx uint8
		switch {
		case healthRatio >= visual.TowerHealthThresholdDamaged:
			paletteIdx = tc.Palette256Healthy
		case healthRatio >= visual.TowerHealthThresholdCritical:
			paletteIdx = tc.Palette256Damaged
		default:
			paletteIdx = tc.Palette256Critical
		}

		buf.SetBg256(screenX, screenY, paletteIdx)
	}
}

// === Basic Color ===

func (r *TowerRenderer) renderBasicColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, headerEntity := range r.gameCtx.World.Components.Tower.GetAllEntities() {
		towerComp, ok := r.gameCtx.World.Components.Tower.GetComponent(headerEntity)
		if !ok {
			continue
		}

		headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headerEntity)
		if !ok {
			continue
		}

		visualType := clampVisualType(towerComp.Type)
		r.renderMembersBasicColor(ctx, buf, &towerComp, &headerComp, visualType)
	}
}

func (r *TowerRenderer) renderMembersBasicColor(
	ctx render.RenderContext,
	buf *render.RenderBuffer,
	towerComp *component.TowerComponent,
	headerComp *component.HeaderComponent,
	visualType int,
) {
	radiusX := float64(towerComp.RadiusX)
	radiusY := float64(towerComp.RadiusY)
	if radiusX < 1 {
		radiusX = 1
	}
	if radiusY < 1 {
		radiusY = 1
	}

	invRxSq := 1.0 / (radiusX * radiusX)
	invRySq := 1.0 / (radiusY * radiusY)

	minHP := towerComp.MinHP
	maxHP := towerComp.MaxHP
	hpRange := maxHP - minHP

	tc := &visual.TowerTypes[visualType]

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
		case healthRatio >= visual.TowerHealthThresholdDamaged:
			colorIdx = tc.BasicHealthy
		case healthRatio >= visual.TowerHealthThresholdCritical:
			colorIdx = tc.BasicDamaged
		default:
			colorIdx = tc.BasicCritical
		}

		buf.SetBg256(screenX, screenY, colorIdx)
	}
}