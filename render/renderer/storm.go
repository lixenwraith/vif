package renderer

import (
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

// stormCellFunc renders a single cell within the storm circle ellipse
type stormCellFunc func(r *StormRenderer, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq, brightness int64, baseColor terminal.RGB)

// StormRenderer draws the storm boss entity with depth-based rendering
type StormRenderer struct {
	gameCtx *engine.GameContext

	// Color mode dispatch
	renderCell stormCellFunc

	// Reusable slice for sorting (avoids allocation per frame)
	sortBuffer []stormCircleRender
}

// NewStormRenderer creates a new storm renderer
func NewStormRenderer(gameCtx *engine.GameContext) *StormRenderer {
	r := &StormRenderer{
		gameCtx:    gameCtx,
		sortBuffer: make([]stormCircleRender, 0, component.StormCircleCount),
	}

	if gameCtx.World.Resources.Render.ColorMode == terminal.ColorMode256 {
		r.renderCell = stormCell256
	} else {
		r.renderCell = stormCellTrueColor
	}

	return r
}

// Render draws all storm circles sorted by depth (back-to-front)
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

// renderStorm draws a single storm entity's circles
func (r *StormRenderer) renderStorm(ctx render.RenderContext, buf *render.RenderBuffer, stormComp *component.StormComponent) {
	// Collect alive circles for sorting
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

	// Sort by Z descending (far objects first, low Z = far = render first)
	sort.Slice(r.sortBuffer, func(i, j int) bool {
		return r.sortBuffer[i].z < r.sortBuffer[j].z
	})

	// Render back-to-front
	for _, circle := range r.sortBuffer {
		r.renderCircle(ctx, buf, circle)
	}
}

// renderCircle draws a single storm circle with depth-based brightness
func (r *StormRenderer) renderCircle(ctx render.RenderContext, buf *render.RenderBuffer, circle stormCircleRender) {
	// Calculate brightness from Z position
	// Z range: StormZMin (near, bright) to StormZMax (far, dark)
	// Map Z to [0, Scale] where 0 = far (dark), Scale = near (bright)
	zRange := parameter.StormZMax - parameter.StormZMin
	if zRange <= 0 {
		zRange = vmath.Scale
	}

	// Invert: higher Z = farther = darker
	// brightness = 1 - ((z - zMin) / zRange)
	zNormalized := vmath.Div(circle.z-parameter.StormZMin, zRange)
	if zNormalized < 0 {
		zNormalized = 0
	}
	if zNormalized > vmath.Scale {
		zNormalized = vmath.Scale
	}
	brightness := vmath.Scale - zNormalized

	// Clamp brightness to [0.3, 1.0] range for visibility
	minBrightness := vmath.FromFloat(0.3)
	brightness = minBrightness + vmath.Mul(brightness, vmath.Scale-minBrightness)

	// Get base color for this circle
	baseColor := visual.StormCircleColors[circle.index%len(visual.StormCircleColors)]

	// Calculate ellipse bounds
	radiusXInt := vmath.ToInt(parameter.StormCircleRadiusX)
	radiusYInt := vmath.ToInt(parameter.StormCircleRadiusY)

	startX := max(0, circle.x-radiusXInt)
	endX := min(ctx.GameWidth-1, circle.x+radiusXInt)
	startY := max(0, circle.y-radiusYInt)
	endY := min(ctx.GameHeight-1, circle.y+radiusYInt)

	// Render ellipse
	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			dx := vmath.FromInt(x - circle.x)
			dy := vmath.FromInt(y - circle.y)

			normalizedDistSq := vmath.EllipseDistSq(dx, dy,
				parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq)

			if normalizedDistSq > vmath.Scale {
				continue
			}

			screenX := ctx.GameXOffset + x
			screenY := ctx.GameYOffset + y

			r.renderCell(r, buf, screenX, screenY, normalizedDistSq, brightness, baseColor)
		}
	}

	// Render halo (outer glow)
	r.renderHalo(ctx, buf, circle, brightness, baseColor)
}

// renderHalo draws a soft glow around the circle
func (r *StormRenderer) renderHalo(ctx render.RenderContext, buf *render.RenderBuffer, circle stormCircleRender, brightness int64, baseColor terminal.RGB) {
	// Halo extends beyond main radius
	haloExtend := 2
	radiusXInt := vmath.ToInt(parameter.StormCircleRadiusX) + haloExtend
	radiusYInt := vmath.ToInt(parameter.StormCircleRadiusY) + haloExtend

	// Precompute halo ellipse inverse radii
	haloRadiusX := parameter.StormCircleRadiusX + vmath.FromInt(haloExtend)
	haloRadiusY := parameter.StormCircleRadiusY + vmath.FromInt(haloExtend)
	haloInvRxSq, haloInvRySq := vmath.EllipseInvRadiiSq(haloRadiusX, haloRadiusY)

	startX := max(0, circle.x-radiusXInt)
	endX := min(ctx.GameWidth-1, circle.x+radiusXInt)
	startY := max(0, circle.y-radiusYInt)
	endY := min(ctx.GameHeight-1, circle.y+radiusYInt)

	for y := startY; y <= endY; y++ {
		for x := startX; x <= endX; x++ {
			dx := vmath.FromInt(x - circle.x)
			dy := vmath.FromInt(y - circle.y)

			// Check if inside main circle (already rendered)
			mainDistSq := vmath.EllipseDistSq(dx, dy,
				parameter.StormCollisionInvRxSq, parameter.StormCollisionInvRySq)
			if mainDistSq <= vmath.Scale {
				continue
			}

			// Check if inside halo region
			haloDistSq := vmath.EllipseDistSq(dx, dy, haloInvRxSq, haloInvRySq)
			if haloDistSq > vmath.Scale {
				continue
			}

			// Halo alpha: fade from inner edge to outer edge
			// mainDistSq > Scale, haloDistSq <= Scale
			// t = (haloDistSq - Scale) / (mainDistSq - Scale) inverted
			haloAlpha := vmath.Scale - haloDistSq
			haloAlpha = vmath.Mul(haloAlpha, brightness)
			haloAlpha = vmath.Mul(haloAlpha, vmath.FromFloat(0.4)) // Max halo opacity

			if haloAlpha <= 0 {
				continue
			}

			screenX := ctx.GameXOffset + x
			screenY := ctx.GameYOffset + y

			buf.Set(screenX, screenY, 0, visual.RgbBlack, baseColor,
				render.BlendAdd, vmath.ToFloat(haloAlpha), terminal.AttrNone)
		}
	}
}

// stormCellTrueColor renders a cell with gradient based on distance from center
func stormCellTrueColor(r *StormRenderer, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq, brightness int64, baseColor terminal.RGB) {
	// Apply brightness to base color
	scaledColor := render.Scale(baseColor, vmath.ToFloat(brightness))

	// Inner gradient: brighter at center, base color at edge
	// t = sqrt(normalizedDistSq) for linear falloff
	edgeFactor := normalizedDistSq // Use squared for faster falloff
	centerBoost := vmath.Scale - edgeFactor
	centerBoost = vmath.Mul(centerBoost, vmath.FromFloat(0.3)) // 30% brighter at center

	finalColor := render.Add(scaledColor, scaledColor, vmath.ToFloat(centerBoost))

	buf.SetBgOnly(screenX, screenY, finalColor)
}

// stormCell256 renders a cell using 256-color palette
func stormCell256(r *StormRenderer, buf *render.RenderBuffer, screenX, screenY int, normalizedDistSq, brightness int64, baseColor terminal.RGB) {
	// Use fixed palette index based on brightness threshold
	// Brighter circles use lighter palette colors
	var paletteIdx uint8
	if brightness > vmath.FromFloat(0.7) {
		paletteIdx = visual.Storm256Bright
	} else if brightness > vmath.FromFloat(0.5) {
		paletteIdx = visual.Storm256Normal
	} else {
		paletteIdx = visual.Storm256Dark
	}

	buf.SetBg256(screenX, screenY, paletteIdx)
}