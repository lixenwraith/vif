package renderer

import (
	"math"
	"sort"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// stormCircleRender holds data for depth-sorted rendering
type stormCircleRender struct {
	entity core.Entity
	x, y   int   // Grid position
	z      int64 // Z depth for sorting (Q32.32)
	index  int   // Circle index for color selection
}

// StormRenderer draws the storm boss entity with depth-based rendering
type StormRenderer struct {
	gameCtx *engine.GameContext

	// Reusable slice for sorting
	sortBuffer []stormCircleRender

	// Precomputed constants
	radiusX, radiusY               float64
	invRadiusX, invRadiusY         float64
	haloRadiusX, haloRadiusY       float64
	glowMaxRadiusX, glowMaxRadiusY float64

	// Attack effect radii
	greenAttackRadiusX, greenAttackRadiusY float64
}

func NewStormRenderer(gameCtx *engine.GameContext) *StormRenderer {
	rx := parameter.StormCircleRadiusXFloat
	ry := parameter.StormCircleRadiusYFloat
	haloExtendX := parameter.StormConcaveHaloExtendFloat
	haloExtendY := haloExtendX * (ry / rx)
	glowExtend := parameter.StormConvexGlowExtendFloat

	return &StormRenderer{
		gameCtx:    gameCtx,
		sortBuffer: make([]stormCircleRender, 0, component.StormCircleCount),

		radiusX:        rx,
		radiusY:        ry,
		invRadiusX:     1.0 / rx,
		invRadiusY:     1.0 / ry,
		haloRadiusX:    rx + haloExtendX,
		haloRadiusY:    ry + haloExtendY,
		glowMaxRadiusX: rx + glowExtend,
		glowMaxRadiusY: ry + glowExtend*(ry/rx),

		greenAttackRadiusX: rx * parameter.StormGreenRadiusMultiplier,
		greenAttackRadiusY: ry * parameter.StormGreenRadiusMultiplier,
	}
}

func (r *StormRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	stormEntities := r.gameCtx.World.Components.Storm.GetAllEntities()
	if len(stormEntities) == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskComposite)

	for _, rootEntity := range stormEntities {
		stormComp, ok := r.gameCtx.World.Components.Storm.GetComponent(rootEntity)
		if !ok {
			continue
		}
		r.renderStorm(ctx, buf, &stormComp)
	}
}

func (r *StormRenderer) renderStorm(ctx render.RenderContext, buf *render.RenderBuffer, stormComp *component.StormComponent) {
	r.sortBuffer = r.sortBuffer[:0]

	for i := 0; i < component.StormCircleCount; i++ {
		if !stormComp.CirclesAlive[i] {
			continue
		}

		circleEntity := stormComp.Circles[i]
		circleComp, ok := r.gameCtx.World.Components.StormCircle.GetComponent(circleEntity)
		if !ok {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(circleEntity)
		if !ok {
			continue
		}

		r.sortBuffer = append(r.sortBuffer, stormCircleRender{
			entity: circleEntity,
			x:      pos.X,
			y:      pos.Y,
			z:      circleComp.Pos3D.Z,
			index:  circleComp.Index,
		})
	}

	if len(r.sortBuffer) == 0 {
		return
	}

	// Sort by Z descending (Far first, Near last) so near circles overlay far ones
	sort.Slice(r.sortBuffer, func(i, j int) bool {
		return r.sortBuffer[i].z > r.sortBuffer[j].z
	})

	for _, circle := range r.sortBuffer {
		r.renderCircle(ctx, buf, &circle)
	}
}

func (r *StormRenderer) renderCircle(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender) {

	// Render attack effects before body (background layer)
	circleComp, ok := r.gameCtx.World.Components.StormCircle.GetComponent(circle.entity)
	if ok && circleComp.AttackState == component.StormCircleAttackActive {
		switch circle.index {
		case 0: // Green - area pulse
			r.renderGreenPulse(ctx, buf, circle, &circleComp)
		case 1: // Red - cone projectile
			r.renderRedCone(ctx, buf, circle, &circleComp)
		case 2: // Blue - orbiting glow
			r.renderBlueGlow(ctx, buf, circle, &circleComp)
		}
	}

	// Depth factor: 0 = far, 1 = near
	zRange := parameter.StormZMax - parameter.StormZMin
	if zRange <= 0 {
		zRange = vmath.Scale
	}
	zNorm := vmath.Div(circle.z-parameter.StormZMin, zRange)
	if zNorm < 0 {
		zNorm = 0
	}
	if zNorm > vmath.Scale {
		zNorm = vmath.Scale
	}
	depthFactor := vmath.ToFloat(vmath.Scale - zNorm)

	// Depth brightness: 0.6 to 1.0
	depthBright := 0.6 + depthFactor*0.4

	// Base color with saturation boost
	baseColor := visual.StormCircleColors[circle.index%len(visual.StormCircleColors)]
	baseR := math.Min(255, float64(baseColor.R)*1.3)
	baseG := math.Min(255, float64(baseColor.G)*1.3)
	baseB := math.Min(255, float64(baseColor.B)*1.3)

	// Convex (near) vs concave (far)
	isConvex := depthFactor > 0.5

	// Render halo (background glow) only if NOT convex (Far/Concave)
	// Front/Convex circles are vulnerable and show no shield halo
	if !isConvex {
		r.renderHalo(ctx, buf, circle, depthBright, baseR, baseG, baseB)
	} else {
		// Render narrow glowing ring for convex (vulnerable) state
		r.renderConvexGlow(ctx, buf, circle, depthBright, baseR, baseG, baseB)
	}

	// Cursor position in viewport coords for lighting
	cursorVX, cursorVY := ctx.CursorViewportPos()

	// Circle center in viewport coords, member loop handles visibility check
	circleVX, circleVY, _ := ctx.MapToViewport(circle.x, circle.y)

	// Per-circle lighting aimed at cursor position
	// If Convex: Track cursor (expensive)
	// If Concave: Use fixed frontal light (cheap) to ensure visibility without calculation cost
	var lightX, lightY, lightZ, halfX, halfY, halfZ float64
	if isConvex {
		lightX, lightY, lightZ, halfX, halfY, halfZ = cursorLighting(cursorVX, cursorVY, circleVX, circleVY)
	} else {
		// Fixed light from viewer direction (0,0,1)
		lightX, lightY, lightZ = 0.0, 0.0, 1.0
		// Half vector for specular (View 0,0,1 + Light 0,0,1) -> 0,0,1
		halfX, halfY, halfZ = 0.0, 0.0, 1.0
	}

	// Render members (sphere body)
	headerComp, ok := r.gameCtx.World.Components.Header.GetComponent(circle.entity)
	if !ok {
		return
	}

	for _, member := range headerComp.MemberEntries {
		if member.Entity == 0 {
			continue
		}

		// Calculate member's map position from circle center + offset
		memberMapX := circle.x + member.OffsetX
		memberMapY := circle.y + member.OffsetY

		screenX, screenY, visible := ctx.MapToScreen(memberMapX, memberMapY)
		if !visible {
			continue
		}

		// Normalized position within ellipse
		nx := float64(member.OffsetX) * r.invRadiusX
		ny := float64(member.OffsetY) * r.invRadiusY
		distSq := nx*nx + ny*ny

		// Members validated at creation via Q32.32; clamp for shading math only
		if distSq > 1.0 {
			distSq = 1.0
		}

		// Sphere surface normal
		nz := math.Sqrt(1.0 - distSq)
		if !isConvex {
			nz = -nz
		}

		// Rim glow - bright at edges
		rim := 1.0 - math.Abs(nz)
		rim = rim * rim * 0.8

		// Core glow - white center
		coreDist := math.Sqrt(distSq) / 0.7
		coreGlow := 0.0
		if coreDist < 1.0 {
			coreGlow = (1.0 - coreDist) * 0.6
		}

		// Blinn-Phong specular
		spec := nx*halfX + ny*halfY + nz*halfZ
		if spec < 0 {
			spec = 0
		}
		spec = math.Pow(spec, 20.0) * 0.9

		// Lambertian diffuse
		diff := nx*lightX + ny*lightY + nz*lightZ
		if diff < 0 {
			diff = 0
		}

		// Combined intensity
		intensity := (0.3 + diff*0.3 + rim*0.4) * depthBright

		red := baseR*intensity + coreGlow*255 + spec*255
		green := baseG*intensity + coreGlow*255 + spec*255
		blue := baseB*intensity + coreGlow*255 + spec*255

		// Clamp
		if red > 255 {
			red = 255
		}
		if green > 255 {
			green = 255
		}
		if blue > 255 {
			blue = 255
		}

		color := terminal.RGB{R: uint8(red), G: uint8(green), B: uint8(blue)}
		buf.SetBgOnly(screenX, screenY, color)
	}
}

// cursorLighting computes per-circle light and half vectors so that the light appears to come from the cursor direction, making each sphere resemble an eye that tracks the cursor
// The fixed lightZ keeps intensity stable while only the angle changes
func cursorLighting(cursorX, cursorY, circleX, circleY int) (lightX, lightY, lightZ, halfX, halfY, halfZ float64) {
	const lightZ0 = 35.0 // fixed depth – controls tracking sensitivity

	lx := float64(cursorX - circleX)
	ly := float64(cursorY - circleY)
	m := math.Sqrt(lx*lx + ly*ly + lightZ0*lightZ0)
	lightX, lightY, lightZ = lx/m, ly/m, lightZ0/m

	// Blinn-Phong half vector: normalize(light + view), view = (0,0,1)
	hx, hy, hz := lightX, lightY, lightZ+1.0
	m = math.Sqrt(hx*hx + hy*hy + hz*hz)
	halfX, halfY, halfZ = hx/m, hy/m, hz/m
	return
}

func (r *StormRenderer) renderHalo(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, depthBright, baseR, baseG, baseB float64) {
	// Bounding box in map coords
	mapStartX := max(0, circle.x-int(r.haloRadiusX)-1)
	mapEndX := min(ctx.MapWidth-1, circle.x+int(r.haloRadiusX)+1)
	mapStartY := max(0, circle.y-int(r.haloRadiusY)-1)
	mapEndY := min(ctx.MapHeight-1, circle.y+int(r.haloRadiusY)+1)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			// Normalized position
			nx := float64(mapX-circle.x) / r.radiusX
			ny := float64(mapY-circle.y) / r.radiusY
			distSq := nx*nx + ny*ny

			// Skip inside main body (rendered by members)
			if distSq <= 1.0 {
				continue
			}

			// Skip outside halo
			haloNx := float64(mapX-circle.x) / r.haloRadiusX
			haloNy := float64(mapY-circle.y) / r.haloRadiusY
			if haloNx*haloNx+haloNy*haloNy > 1.0 {
				continue
			}

			// Exponential falloff from body edge
			dist := math.Sqrt(distSq)
			glowDist := dist - 1.0
			glowFalloff := math.Exp(-glowDist*3.0) * 0.5 * depthBright

			red := baseR * glowFalloff
			green := baseG * glowFalloff
			blue := baseB * glowFalloff

			if red < 1 && green < 1 && blue < 1 {
				continue
			}

			color := terminal.RGB{R: uint8(red), G: uint8(green), B: uint8(blue)}

			// BlendAdd for glow on black background
			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, 1.0, terminal.AttrNone)
		}
	}
}

func (r *StormRenderer) renderConvexGlow(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, depthBright, baseR, baseG, baseB float64) {
	mapStartX := max(0, circle.x-int(r.glowMaxRadiusX)-1)
	mapEndX := min(ctx.MapWidth-1, circle.x+int(r.glowMaxRadiusX)+1)
	mapStartY := max(0, circle.y-int(r.glowMaxRadiusY)-1)
	mapEndY := min(ctx.MapHeight-1, circle.y+int(r.glowMaxRadiusY)+1)

	// Pulse via GameTime and vmath.Sin LUT
	// angle ∈ [0, Scale) maps to [0, 2π), Sin returns [-Scale, Scale]
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	periodMs := int64(parameter.StormConvexGlowPeriodMs)
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	sinVal := vmath.Sin(angleFixed)          // Q32.32 in [-Scale, Scale]
	pulse := 0.5 + 0.5*vmath.ToFloat(sinVal) // Normalized to [0, 1]

	glowIntensity := depthBright * (parameter.StormConvexGlowIntensityMin +
		(parameter.StormConvexGlowIntensityMax-parameter.StormConvexGlowIntensityMin)*pulse)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			nx := float64(mapX-circle.x) * r.invRadiusX
			ny := float64(mapY-circle.y) * r.invRadiusY
			distSq := nx*nx + ny*ny

			if distSq <= 1.0 || distSq > parameter.StormConvexGlowOuterDistSqFloat {
				continue
			}

			dist := math.Sqrt(distSq)
			alpha := 1.0 - (dist-1.0)*parameter.StormConvexGlowFalloffMult
			if alpha <= 0 {
				continue
			}

			factor := glowIntensity * alpha
			rVal := baseR * factor
			gVal := baseG * factor
			bVal := baseB * factor

			if rVal > 255 {
				rVal = 255
			}
			if gVal > 255 {
				gVal = 255
			}
			if bVal > 255 {
				bVal = 255
			}
			if rVal < 1 && gVal < 1 && bVal < 1 {
				continue
			}

			color := terminal.RGB{R: uint8(rVal), G: uint8(gVal), B: uint8(bVal)}
			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, 1.0, terminal.AttrNone)
		}
	}
}

// renderGreenPulse draws expanding green pulse effect
func (r *StormRenderer) renderGreenPulse(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, circleComp *component.StormCircleComponent) {
	progress := circleComp.AttackProgress
	if progress <= 0 {
		return
	}

	// Pulse expands then fades
	pulsePhase := progress * 2.0
	var radiusMult, alpha float64
	if pulsePhase < 1.0 {
		radiusMult = 0.5 + 0.5*pulsePhase
		alpha = 0.7 * pulsePhase
	} else {
		radiusMult = 1.0
		alpha = 0.7 * (2.0 - pulsePhase)
	}

	if alpha <= 0.02 {
		return
	}

	effectRadiusX := r.greenAttackRadiusX * radiusMult
	effectRadiusY := r.greenAttackRadiusY * radiusMult
	invRx := 1.0 / effectRadiusX
	invRy := 1.0 / effectRadiusY

	mapStartX := max(0, circle.x-int(effectRadiusX)-1)
	mapEndX := min(ctx.MapWidth-1, circle.x+int(effectRadiusX)+1)
	mapStartY := max(0, circle.y-int(effectRadiusY)-1)
	mapEndY := min(ctx.MapHeight-1, circle.y+int(effectRadiusY)+1)

	pulseColor := visual.RgbStormGreenPulse

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			nx := float64(mapX-circle.x) * invRx
			ny := float64(mapY-circle.y) * invRy
			distSq := nx*nx + ny*ny

			if distSq > 1.0 {
				continue
			}

			dist := math.Sqrt(distSq)
			edgeFalloff := 1.0 - dist*dist
			cellAlpha := alpha * edgeFalloff

			if cellAlpha < 0.03 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, pulseColor, render.BlendAdd, cellAlpha, terminal.AttrNone)
		}
	}
}

// renderRedCone draws short muzzle flash effect at circle edge
func (r *StormRenderer) renderRedCone(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, circleComp *component.StormCircleComponent) {
	if circleComp.AttackProgress <= 0 {
		return
	}

	targetX := circleComp.LockedTargetX
	targetY := circleComp.LockedTargetY

	dx := float64(targetX - circle.x)
	dy := float64(targetY - circle.y)
	dist := math.Sqrt(dx*dx + dy*dy)
	if dist < 1 {
		return
	}
	dirX, dirY := dx/dist, dy/dist

	// Spawn point at ellipse exterior
	angle := math.Atan2(dy, dx)
	spawnX := float64(circle.x) + r.radiusX*math.Cos(angle)
	spawnY := float64(circle.y) + r.radiusY*math.Sin(angle)

	// Short muzzle flash: 4 cells, narrow wedge
	const flashLength = 4.0
	const halfAngle = 0.20 // ~11°

	perpX, perpY := -dirY, dirX

	// Time-based flicker using game time
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	flicker := 0.85 + 0.15*vmath.ToFloat(vmath.Sin((gameTimeMs*vmath.Scale/80)%vmath.Scale))

	for step := 0; step <= int(flashLength); step++ {
		t := float64(step) / flashLength
		axisX := spawnX + dirX*float64(step)
		axisY := spawnY + dirY*float64(step)

		// Width expands slightly then tapers
		widthMult := (1.0 - t*0.3) * math.Tan(halfAngle) * float64(step)
		if widthMult < 0.1 {
			widthMult = 0.1
		}

		// Sample across width with some jitter
		widthSteps := int(widthMult*2) + 1
		for w := -widthSteps; w <= widthSteps; w++ {
			wFrac := float64(w) / float64(widthSteps+1)

			// Add chaotic displacement for flame effect
			jitter := vmath.ToFloat(vmath.Sin((gameTimeMs/20+int64(step*7+w*13))*vmath.Scale/100%vmath.Scale)) * 0.3
			cellX := axisX + perpX*(wFrac+jitter)*widthMult
			cellY := axisY + perpY*(wFrac+jitter)*widthMult

			mapX, mapY := int(cellX+0.5), int(cellY+0.5)
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			// Steep falloff: (1-t)^2.5 with edge softening
			edgeDist := math.Abs(wFrac)
			alpha := math.Pow(1.0-t, 2.5) * (1.0 - edgeDist*edgeDist) * flicker
			if alpha < 0.05 {
				continue
			}

			// Color: orange-red at base fading to dark red
			color := render.Lerp(visual.RgbMuzzleFlashBase, visual.RgbMuzzleFlashTip, t)

			buf.Set(screenX, screenY, 0, visual.RgbBlack, color, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// renderBlueBeam renamed and rewritten - replace entire function
func (r *StormRenderer) renderBlueGlow(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, circleComp *component.StormCircleComponent) {
	progress := circleComp.AttackProgress
	if progress <= 0.01 {
		return
	}

	// Fade out during materialize phase (last 20%)
	intensityMult := 1.0
	if progress > 0.8 {
		intensityMult = (1.0 - progress) / 0.2
	}
	if intensityMult <= 0.05 {
		return
	}

	// Fast rotation via game time
	gameTimeMs := r.gameCtx.World.Resources.Time.GameTime.UnixMilli()
	periodMs := parameter.StormBlueGlowRotationPeriod.Milliseconds()
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	cosA := vmath.Cos(angleFixed)
	sinA := vmath.Sin(angleFixed)

	// Warm amber glow contrasting blue body
	glowColor := terminal.RGB{R: 255, G: 190, B: 70}

	// Render ring around circle edge
	outerRx := r.radiusX + parameter.StormConvexGlowExtendFloat + 1.0
	outerRy := r.radiusY + (parameter.StormConvexGlowExtendFloat+1.0)*(r.radiusY/r.radiusX)

	mapStartX := max(0, circle.x-int(outerRx)-1)
	mapEndX := min(ctx.MapWidth-1, circle.x+int(outerRx)+1)
	mapStartY := max(0, circle.y-int(outerRy)-1)
	mapEndY := min(ctx.MapHeight-1, circle.y+int(outerRy)+1)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			nx := float64(mapX-circle.x) * r.invRadiusX
			ny := float64(mapY-circle.y) * r.invRadiusY
			distSq := nx*nx + ny*ny

			// Ring zone: 0.6 to 1.5 normalized radius
			if distSq < 0.36 || distSq > 2.25 {
				continue
			}

			dist := math.Sqrt(distSq)
			cellDirX := nx / dist
			cellDirY := ny / dist

			dirXFixed := vmath.FromFloat(cellDirX)
			dirYFixed := vmath.FromFloat(cellDirY)

			// Double glow: dot with both opposite directions
			dot1 := vmath.ToFloat(vmath.DotProduct(dirXFixed, dirYFixed, cosA, sinA))
			dot2 := vmath.ToFloat(vmath.DotProduct(dirXFixed, dirYFixed, -cosA, -sinA))

			dot := math.Max(dot1, dot2)
			if dot <= 0.1 {
				continue
			}

			// Edge proximity: strongest near edge (dist=1.0)
			edgeFactor := 1.0 - math.Abs(dist-1.0)*1.5
			if edgeFactor <= 0 {
				continue
			}

			// Tight glow with power curve
			alpha := math.Pow(dot, 1.5) * edgeFactor * intensityMult * 0.85
			if alpha < 0.05 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, glowColor, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}