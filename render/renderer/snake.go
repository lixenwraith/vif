package renderer

import (
	"fmt"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// snakeBodyColorEntry holds pre-computed color for segment position
type snakeBodyColorEntry struct {
	center terminal.RGB
	edge   terminal.RGB
}

// snakeRenderFunc defines the render strategy signature
type snakeRenderFunc func(r *SnakeRenderer, ctx render.RenderContext, buf *render.RenderBuffer)

// SnakeRenderer draws snake entities with body gradients and shield glow
type SnakeRenderer struct {
	gameCtx   *engine.GameContext
	colorMode terminal.ColorMode

	// Pre-computed body color gradient LUT (segment index → color pair)
	// Index 0 = head-adjacent, index 255 = max tail
	bodyColorLUT [256]snakeBodyColorEntry

	renderFunc snakeRenderFunc
}

func NewSnakeRenderer(gameCtx *engine.GameContext) *SnakeRenderer {
	colorMode := gameCtx.World.Resources.Config.ColorMode

	r := &SnakeRenderer{
		gameCtx:   gameCtx,
		colorMode: colorMode,
	}

	r.buildBodyColorLUT()

	switch colorMode {
	case terminal.ColorModeTrueColor:
		r.renderFunc = (*SnakeRenderer).renderTrueColor
	case terminal.ColorMode256:
		r.renderFunc = (*SnakeRenderer).render256Color
	default:
		r.renderFunc = (*SnakeRenderer).renderBasicColor
	}

	return r
}

func (r *SnakeRenderer) buildBodyColorLUT() {
	for i := 0; i < 256; i++ {
		t := float64(i) / 255.0
		// Longitudinal gradient: bright → dark toward tail
		darkenFactor := 1.0 - t*visual.SnakeBodyTailDarken
		center := render.Scale(visual.RgbSnakeBodyBright, darkenFactor)
		edge := render.Scale(center, 1.0-visual.SnakeBodyEdgeFalloff)
		r.bodyColorLUT[i] = snakeBodyColorEntry{center: center, edge: edge}
	}
}

func (r *SnakeRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	snakeEntities := r.gameCtx.World.Components.Snake.GetAllEntities()
	if len(snakeEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)
	r.renderFunc(r, ctx, buf)
}

func (r *SnakeRenderer) renderTrueColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, rootEntity := range r.gameCtx.World.Components.Snake.GetAllEntities() {
		snakeComp, ok := r.gameCtx.World.Components.Snake.GetComponent(rootEntity)
		if !ok {
			continue
		}

		headPos, ok := r.gameCtx.World.Positions.GetPosition(snakeComp.HeadEntity)
		if !ok {
			continue
		}

		// Shield glow (background layer)
		if snakeComp.IsShielded {
			r.renderShieldGlow(ctx, buf, headPos.X, headPos.Y)
		}

		// Body segments
		if snakeComp.BodyEntity != 0 {
			r.renderBodyTrueColor(ctx, buf, snakeComp.BodyEntity)
		}

		// Head members (foreground)
		r.renderHeadTrueColor(ctx, buf, snakeComp.HeadEntity, snakeComp.IsShielded)
	}
}

func (r *SnakeRenderer) renderShieldGlow(ctx render.RenderContext, buf *render.RenderBuffer, centerX, centerY int) {
	glowExtend := visual.SnakeShieldGlowExtend

	// Head is 5×3 cells. Terminal chars are ~2:1 aspect (height:width)
	// For visually circular glow: radiusY in cells = radiusX / 2
	baseRadiusX := float64(parameter.SnakeHeadWidth) / 2.0
	baseRadiusY := float64(parameter.SnakeHeadHeight) / 2.0

	// Glow extends beyond head bounds
	glowRadiusX := baseRadiusX + glowExtend
	glowRadiusY := baseRadiusY + glowExtend/2.0 // Half extension for Y due to aspect

	// Pre-compute inverse squared for ellipse distance
	invRxSq := vmath.FromFloat(1.0 / (glowRadiusX * glowRadiusX))
	invRySq := vmath.FromFloat(1.0 / (glowRadiusY * glowRadiusY))

	// Inner ellipse (head bounds) for exclusion
	innerRadiusX := baseRadiusX * 0.8
	innerRadiusY := baseRadiusY * 0.8
	invInnerRxSq := vmath.FromFloat(1.0 / (innerRadiusX * innerRadiusX))
	invInnerRySq := vmath.FromFloat(1.0 / (innerRadiusY * innerRadiusY))

	// Pulse intensity
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	periodMs := int64(parameter.StormConvexGlowPeriodMs)
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	sinVal := vmath.Sin(angleFixed)
	pulse := 0.5 + 0.5*vmath.ToFloat(sinVal)
	glowIntensity := 0.3 + 0.4*pulse

	// Bounding box
	mapStartX := max(0, centerX-int(glowRadiusX)-1)
	mapEndX := min(ctx.MapWidth-1, centerX+int(glowRadiusX)+1)
	mapStartY := max(0, centerY-int(glowRadiusY)-1)
	mapEndY := min(ctx.MapHeight-1, centerY+int(glowRadiusY)+1)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := vmath.FromInt(mapX - centerX)
			dy := vmath.FromInt(mapY - centerY)

			// Ellipse distance: (dx/rx)² + (dy/ry)²
			outerDistSq := vmath.EllipseDistSq(dx, dy, invRxSq, invRySq)
			innerDistSq := vmath.EllipseDistSq(dx, dy, invInnerRxSq, invInnerRySq)

			// Skip inside head or outside glow
			if innerDistSq <= vmath.Scale || outerDistSq > vmath.Scale {
				continue
			}

			// Falloff from inner edge toward outer edge
			innerDist := vmath.Sqrt(innerDistSq)

			// Alpha: strongest at inner edge, fades to outer
			t := vmath.ToFloat(vmath.Div(innerDist-vmath.Scale, vmath.Scale))
			alpha := 1.0 - t
			if alpha <= 0 {
				continue
			}

			factor := glowIntensity * alpha * alpha // Quadratic falloff
			color := render.Scale(visual.RgbSnakeShieldTint, factor)
			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, 1.0, terminal.AttrNone)
		}
	}
}

func (r *SnakeRenderer) renderBodyTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, bodyEntity core.Entity) {
	bodyComp, ok := r.gameCtx.World.Components.SnakeBody.GetComponent(bodyEntity)
	if !ok {
		return
	}

	segmentCount := len(bodyComp.Segments)
	if segmentCount == 0 {
		return
	}

	resolved := r.resolveBodyMembers(bodyEntity, segmentCount)
	connectedCount := r.countConnectedSegments(&bodyComp, resolved)
	if connectedCount == 0 {
		return
	}

	// Lateral offsets matching resolved index order: center, left, right
	lateralOffsets := [3]int{0, -1, 1}

	connectedIdx := 0
	for i := range bodyComp.Segments {
		seg := &bodyComp.Segments[i]
		if !seg.Connected || !resolved[i].hasAny {
			continue
		}

		// LUT index based on connected segment position
		lutIdx := 0
		if connectedCount > 1 {
			lutIdx = (connectedIdx * 255) / (connectedCount - 1)
		}
		if lutIdx > 255 {
			lutIdx = 255
		}
		entry := r.bodyColorLUT[lutIdx]
		connectedIdx++

		for j, memberEntity := range resolved[i].members {
			if memberEntity == 0 {
				continue
			}

			combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(memberEntity)
			if !ok || combatComp.HitPoints <= 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(memberEntity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			// Base color from lateral position
			var baseColor terminal.RGB
			if lateralOffsets[j] == 0 {
				baseColor = entry.center
			} else {
				baseColor = entry.edge
			}

			// Health modulation
			maxHP := parameter.CombatInitialHPSnakeMemberMin
			if snakeMemberComp, ok := r.gameCtx.World.Components.SnakeMember.GetComponent(memberEntity); ok && snakeMemberComp.MaxHitPoints > 0 {
				maxHP = snakeMemberComp.MaxHitPoints
			}
			healthRatio := float64(combatComp.HitPoints) / float64(maxHP)
			if healthRatio > 1.0 {
				healthRatio = 1.0
			}
			color := render.Scale(baseColor, 0.3+0.7*healthRatio)

			// Hit flash
			if combatComp.RemainingHitFlash > 0 {
				color = r.calculateFlashColor(color, combatComp.RemainingHitFlash)
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendMax, 1.0, terminal.AttrNone)
		}
	}
}

func (r *SnakeRenderer) renderHeadTrueColor(ctx render.RenderContext, buf *render.RenderBuffer, headEntity core.Entity, isShielded bool) {
	headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headEntity)
	if !ok {
		return
	}

	r.gameCtx.World.DebugPrint(fmt.Sprintf("%d", len(headerComp.MemberEntries)))

	combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(headEntity)
	if !ok {
		return
	}

	// Head color: shielded tint or health-based
	var baseColor terminal.RGB
	if isShielded {
		baseColor = render.Lerp(visual.RgbSnakeHeadBright, visual.RgbSnakeShieldTint, 0.3)
	} else {
		healthRatio := float64(combatComp.HitPoints) / float64(parameter.CombatInitialHPSnakeHead)
		if healthRatio > 1.0 {
			healthRatio = 1.0
		}
		baseColor = render.Lerp(visual.RgbSnakeHeadDark, visual.RgbSnakeHeadBright, healthRatio)
	}

	// Hit flash override (only when unshielded)
	if !isShielded && combatComp.RemainingHitFlash > 0 {
		baseColor = r.calculateFlashColor(baseColor, combatComp.RemainingHitFlash)
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

		// Character from grid
		row := member.OffsetY + parameter.SnakeHeadHeaderOffsetY
		col := member.OffsetX + parameter.SnakeHeadHeaderOffsetX
		if row < 0 || row >= parameter.SnakeHeadHeight || col < 0 || col >= parameter.SnakeHeadWidth {
			continue
		}
		ch := visual.SnakeHeadChars[row][col]

		buf.SetFgOnly(screenX, screenY, ch, baseColor, terminal.AttrNone)
	}
}

func (r *SnakeRenderer) calculateFlashColor(base terminal.RGB, remaining time.Duration) terminal.RGB {
	progress := float64(remaining) / float64(parameter.CombatHitFlashDuration)

	var intensity float64
	if progress > 0.67 {
		intensity = 0.6
	} else if progress > 0.33 {
		intensity = 1.0
	} else {
		intensity = 0.6
	}

	flashColor := render.Scale(visual.RgbCombatHitFlash, intensity)
	return render.Lerp(base, flashColor, visual.SnakeHitFlashIntensity)
}

func (r *SnakeRenderer) countConnectedSegments(bodyComp *component.SnakeBodyComponent, resolved []snakeResolvedSegment) int {
	count := 0
	for i := range bodyComp.Segments {
		if bodyComp.Segments[i].Connected && resolved[i].hasAny {
			count++
		}
	}
	return count
}

// --- 256-Color Path ---

func (r *SnakeRenderer) render256Color(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, rootEntity := range r.gameCtx.World.Components.Snake.GetAllEntities() {
		snakeComp, ok := r.gameCtx.World.Components.Snake.GetComponent(rootEntity)
		if !ok {
			continue
		}

		// Body
		if snakeComp.BodyEntity != 0 {
			r.renderBody256Color(ctx, buf, snakeComp.BodyEntity)
		}

		// Head
		r.renderHead256Color(ctx, buf, snakeComp.HeadEntity)
	}
}

func (r *SnakeRenderer) renderBody256Color(ctx render.RenderContext, buf *render.RenderBuffer, bodyEntity core.Entity) {
	bodyComp, ok := r.gameCtx.World.Components.SnakeBody.GetComponent(bodyEntity)
	if !ok {
		return
	}

	segmentCount := len(bodyComp.Segments)
	if segmentCount == 0 {
		return
	}

	resolved := r.resolveBodyMembers(bodyEntity, segmentCount)

	for i := range bodyComp.Segments {
		seg := &bodyComp.Segments[i]
		if !seg.Connected || !resolved[i].hasAny {
			continue
		}

		var paletteIdx uint8
		if segmentCount > 1 {
			t := float64(i) / float64(segmentCount-1)
			if t < 0.5 {
				paletteIdx = visual.Snake256BodyFront
			} else {
				paletteIdx = visual.Snake256BodyBack
			}
		} else {
			paletteIdx = visual.Snake256BodyFront
		}

		for _, memberEntity := range resolved[i].members {
			if memberEntity == 0 {
				continue
			}

			combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(memberEntity)
			if !ok || combatComp.HitPoints <= 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(memberEntity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			buf.SetBg256(screenX, screenY, paletteIdx)
		}
	}
}

func (r *SnakeRenderer) renderHead256Color(ctx render.RenderContext, buf *render.RenderBuffer, headEntity core.Entity) {
	headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headEntity)
	if !ok {
		return
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

		buf.SetBg256(screenX, screenY, visual.Snake256Head)
	}
}

// --- Basic Color Path ---

func (r *SnakeRenderer) renderBasicColor(ctx render.RenderContext, buf *render.RenderBuffer) {
	for _, rootEntity := range r.gameCtx.World.Components.Snake.GetAllEntities() {
		snakeComp, ok := r.gameCtx.World.Components.Snake.GetComponent(rootEntity)
		if !ok {
			continue
		}

		// Body
		if snakeComp.BodyEntity != 0 {
			r.renderBodyBasicColor(ctx, buf, snakeComp.BodyEntity)
		}

		// Head
		r.renderHeadBasicColor(ctx, buf, snakeComp.HeadEntity)
	}
}

func (r *SnakeRenderer) renderBodyBasicColor(ctx render.RenderContext, buf *render.RenderBuffer, bodyEntity core.Entity) {
	bodyComp, ok := r.gameCtx.World.Components.SnakeBody.GetComponent(bodyEntity)
	if !ok {
		return
	}

	segmentCount := len(bodyComp.Segments)
	if segmentCount == 0 {
		return
	}

	resolved := r.resolveBodyMembers(bodyEntity, segmentCount)

	for i := range bodyComp.Segments {
		seg := &bodyComp.Segments[i]
		if !seg.Connected || !resolved[i].hasAny {
			continue
		}

		for _, memberEntity := range resolved[i].members {
			if memberEntity == 0 {
				continue
			}

			combatComp, ok := r.gameCtx.World.Components.Combat.GetComponent(memberEntity)
			if !ok || combatComp.HitPoints <= 0 {
				continue
			}

			pos, ok := r.gameCtx.World.Positions.GetPosition(memberEntity)
			if !ok {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(pos.X, pos.Y)
			if !visible {
				continue
			}

			buf.SetBg256(screenX, screenY, visual.SnakeBasicBody)
		}
	}
}

func (r *SnakeRenderer) renderHeadBasicColor(ctx render.RenderContext, buf *render.RenderBuffer, headEntity core.Entity) {
	headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(headEntity)
	if !ok {
		return
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

		buf.SetBg256(screenX, screenY, visual.SnakeBasicHead)
	}
}

// --- Segment resolution ---

// snakeResolvedSegment holds resolved member entities for one body segment
// Indices: 0=center (lateral 0), 1=left (lateral -1), 2=right (lateral 1)
type snakeResolvedSegment struct {
	members [3]core.Entity
	hasAny  bool
}

// resolveBodyMembers builds per-segment member mapping from HeaderComponent + SnakeMemberComponent
func (r *SnakeRenderer) resolveBodyMembers(bodyEntity core.Entity, segmentCount int) []snakeResolvedSegment {
	resolved := make([]snakeResolvedSegment, segmentCount)

	headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(bodyEntity)
	if !ok {
		return resolved
	}

	for _, entry := range headerComp.MemberEntries {
		if entry.Entity == 0 {
			continue
		}
		sm, ok := r.gameCtx.World.Components.SnakeMember.GetComponent(entry.Entity)
		if !ok || sm.SegmentIndex >= segmentCount {
			continue
		}

		var idx int
		switch sm.LateralOffset {
		case 0:
			idx = 0
		case -1:
			idx = 1
		case 1:
			idx = 2
		default:
			continue
		}

		resolved[sm.SegmentIndex].members[idx] = entry.Entity
		resolved[sm.SegmentIndex].hasAny = true
	}

	return resolved
}