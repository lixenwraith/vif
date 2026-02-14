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
		case 2: // Cyan - orbiting glow
			r.renderCyanGlow(ctx, buf, circle, &circleComp)
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

// renderRedCone draws traveling cone projectile effect with aspect correction
func (r *StormRenderer) renderRedCone(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, circleComp *component.StormCircleComponent) {
	progress := circleComp.AttackProgress
	if progress <= 0 {
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

	coneHeight := float64(parameter.StormRedConeHeightCells)
	halfWidth := float64(parameter.StormRedConeWidthCells) / 2.0

	// Two-phase tail/front calculation
	var tailDist, frontDist float64
	tailFrac := parameter.StormRedConeTailFraction
	if progress < tailFrac {
		tailDist = 0
		frontDist = coneHeight * progress / tailFrac
	} else {
		tailDist = coneHeight * (progress - tailFrac) / (1.0 - tailFrac)
		frontDist = coneHeight
	}

	if frontDist <= tailDist {
		return
	}

	coneColor := visual.RgbStormRedCone

	// Grid perpendicular direction
	perpX, perpY := -dirY, dirX
	// Visual magnitude for uniform stepping (terminal 2:1 aspect)
	perpVisualMag := math.Sqrt(perpX*perpX + 4.0*perpY*perpY)
	if perpVisualMag < 0.001 {
		perpVisualMag = 1.0
	}

	// Fade parameters
	tailFadeStart := parameter.StormRedConeFadeStart
	frontFadeZone := 0.15 // Front 15% of cone length fades toward tip

	coneLength := frontDist - tailDist
	if coneLength < 1 {
		coneLength = 1
	}

	// Sample along cone length from tail to front
	steps := int(frontDist-tailDist) + 1
	for step := 0; step <= steps; step++ {
		stepDist := tailDist + float64(step)
		if stepDist > frontDist {
			stepDist = frontDist
		}

		axisX := float64(circle.x) + dirX*stepDist
		axisY := float64(circle.y) + dirY*stepDist

		// Width at this depth (linear expansion from cone origin)
		widthRatio := stepDist / coneHeight
		currentHalfWidth := halfWidth * widthRatio
		if currentHalfWidth < 1.0 {
			currentHalfWidth = 1.0
		}

		// Position within current cone segment [0=tail, 1=front]
		posInCone := (stepDist - tailDist) / coneLength

		// Base alpha with tail fade
		stepAlpha := 0.7
		tailRatio := 1.0 - posInCone // 1 at tail, 0 at front
		if tailRatio > tailFadeStart {
			fadeAmount := (tailRatio - tailFadeStart) / (1.0 - tailFadeStart)
			stepAlpha *= 1.0 - fadeAmount*0.6
		}

		// Front-tip fade: reduce alpha near the leading edge
		frontRatio := posInCone // 0 at tail, 1 at front
		if frontRatio > (1.0 - frontFadeZone) {
			fadeAmount := (frontRatio - (1.0 - frontFadeZone)) / frontFadeZone
			stepAlpha *= 1.0 - fadeAmount*0.5
		}

		// Sample across width in visual-uniform units
		for vOff := -currentHalfWidth; vOff <= currentHalfWidth; vOff += 1.0 {
			gridOff := vOff / perpVisualMag
			mapX := int(math.Round(axisX + perpX*gridOff))
			mapY := int(math.Round(axisY + perpY*gridOff))

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			// Lateral falloff from axis
			axisDist := math.Abs(vOff) / (currentHalfWidth + 0.1)
			alpha := stepAlpha * (1.0 - axisDist*axisDist*0.4)
			if alpha < 0.03 {
				continue
			}

			buf.Set(screenX, screenY, 0, visual.RgbBlack, coneColor, render.BlendAdd, alpha, terminal.AttrNone)
		}
	}
}

// renderCyanBeam renamed and rewritten - replace entire function
func (r *StormRenderer) renderCyanGlow(ctx render.RenderContext, buf *render.RenderBuffer, circle *stormCircleRender, circleComp *component.StormCircleComponent) {
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
	periodMs := parameter.StormCyanGlowRotationPeriod.Milliseconds()
	angleFixed := ((gameTimeMs % periodMs) * vmath.Scale) / periodMs
	cosA := vmath.Cos(angleFixed)
	sinA := vmath.Sin(angleFixed)

	// Warm amber glow contrasting cyan body
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