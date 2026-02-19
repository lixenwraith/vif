package renderer

import (
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// emberLayerColors holds pre-blended intensities for cached 1D mapping
type emberLayerColors struct {
	Core terminal.RGB
	Mid  terminal.RGB
	Edge terminal.RGB
}

// emberCellFunc renders a single cell within the ember ellipse
type emberCellFunc func(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, dx, dy int64)

// EmberPainter handles per-cell rendering with color mode dispatch
type EmberPainter struct {
	renderCell emberCellFunc

	// Per-Paint state
	params   visual.EmberParams
	colors   emberColors
	gameTime int64
	radiusX  int64
	radiusY  int64

	// Ring rotation state (computed once per paint)
	ringAngles [visual.EmberRingCount]int64

	// Caching and Precalculation States
	lastHeat       int
	colorLUT       [256]emberLayerColors
	invRadiiSqLUT  [256]struct{ invRxSq, invRySq int64 }
	ringInvWidthSq float64
}

// EmberRenderer renders ember effect for entities with active ember state
type EmberRenderer struct {
	gameCtx *engine.GameContext
	painter *EmberPainter
}

// NewEmberRenderer creates the ember system renderer
func NewEmberRenderer(gameCtx *engine.GameContext) *EmberRenderer {
	return &EmberRenderer{
		gameCtx: gameCtx,
		painter: NewEmberPainter(gameCtx.World.Resources.Config.ColorMode),
	}
}

// Render draws all active ember effects
func (r *EmberRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskField)

	shieldEntities := r.gameCtx.World.Components.Shield.GetAllEntities()
	if len(shieldEntities) == 0 {
		return
	}

	cursorEntity := r.gameCtx.World.Resources.Player.Entity

	for _, entity := range shieldEntities {
		heatComp, ok := r.gameCtx.World.Components.Heat.GetComponent(entity)
		if !ok || !heatComp.EmberActive {
			continue
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			continue
		}

		skipX, skipY := -1, -1
		if entity == cursorEntity {
			skipX = pos.X
			skipY = pos.Y
		}

		r.painter.Paint(buf, ctx, pos.X, pos.Y, heatComp.Current, skipX, skipY)
	}
}

// emberColors holds interpolated colors for current heat level
type emberColors struct {
	Core terminal.RGB
	Mid  terminal.RGB
	Edge terminal.RGB
	Ring terminal.RGB
}

// interpolateEmberColors computes colors for given heat factor (Q32.32)
func interpolateEmberColors(t int64) emberColors {
	return emberColors{
		Core: render.LerpRGBFixed(visual.RgbEmberCoreLow, visual.RgbEmberCoreHigh, t),
		Mid:  render.LerpRGBFixed(visual.RgbEmberMidLow, visual.RgbEmberMidHigh, t),
		Edge: render.LerpRGBFixed(visual.RgbEmberEdgeLow, visual.RgbEmberEdgeHigh, t),
		Ring: render.LerpRGBFixed(visual.RgbEmberRingLow, visual.RgbEmberRingHigh, t),
	}
}

// NewEmberPainter creates a painter for the specified color mode
func NewEmberPainter(colorMode terminal.ColorMode) *EmberPainter {
	p := &EmberPainter{
		radiusX:  visual.EmberRadiusX,
		radiusY:  visual.EmberRadiusY,
		lastHeat: -1, // Force cache rebuild on first frame
	}
	if colorMode == terminal.ColorMode256 {
		p.renderCell = emberCell256
	} else {
		p.renderCell = emberCellTrueColor
	}
	return p
}

// Paint renders the ember effect centered at (centerX, centerY) in map coordinates
func (p *EmberPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, heat int, skipX, skipY int) {
	p.gameTime = ctx.GameTime.UnixNano()

	// 1D Cache Rebuild: Only on heat change
	if heat != p.lastHeat {
		p.lastHeat = heat
		p.params = visual.InterpolateEmberParams(heat)
		p.colors = interpolateEmberColors(p.params.HeatFactor)
		p.buildColorLUT()
	}

	// Precalculate ring width inversion
	widthF := vmath.ToFloat(p.params.RingWidth)
	if widthF > 0 {
		p.ringInvWidthSq = 1.0 / (widthF * widthF)
	} else {
		p.ringInvWidthSq = 0
	}

	// Compute ring rotation angles based on game time
	for i := 0; i < visual.EmberRingCount; i++ {
		period := time.Second.Nanoseconds()
		phase := (p.gameTime * vmath.Mul(p.params.RingSpeed, vmath.Scale)) / period
		p.ringAngles[i] = vmath.NormalizeAngle(phase + visual.EmberRingPhaseOffsets[i])
	}

	// Precalculate Jagged Noise & Geometric Divisions for the frame
	for i := 0; i < 256; i++ {
		theta := (int64(i) * vmath.Scale) / 256
		disp := p.computeJaggedDisplacement(theta)
		adjRx := p.radiusX + disp
		adjRy := p.radiusY + vmath.Div(disp, 2*vmath.Scale)
		invRxSq, invRySq := vmath.EllipseInvRadiiSq(adjRx, adjRy)
		p.invRadiiSqLUT[i].invRxSq = invRxSq
		p.invRadiiSqLUT[i].invRySq = invRySq
	}

	// Bounding box in map coords with margin for jagged edges
	margin := vmath.ToInt(p.params.JaggedAmp) + 2
	radiusXInt := vmath.ToInt(p.radiusX)
	radiusYInt := vmath.ToInt(p.radiusY)

	mapStartX := max(0, centerX-radiusXInt-margin)
	mapEndX := min(ctx.MapWidth-1, centerX+radiusXInt+margin)
	mapStartY := max(0, centerY-radiusYInt-margin)
	mapEndY := min(ctx.MapHeight-1, centerY+radiusYInt+margin)

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			if mapX == skipX && mapY == skipY {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			dx := vmath.FromInt(mapX - centerX)
			dy := vmath.FromInt(mapY - centerY)

			// Fast geometric verification mapping direction to cached displacement inversion
			theta := vmath.Atan2(dy, dx)
			lutIdx := (theta >> (vmath.Shift - 8)) & 255
			invRxSq := p.invRadiiSqLUT[lutIdx].invRxSq
			invRySq := p.invRadiiSqLUT[lutIdx].invRySq

			normDistSq := vmath.EllipseDistSq(dx, dy, invRxSq, invRySq)

			if normDistSq > vmath.Scale+vmath.Scale/4 {
				continue
			}

			p.renderCell(p, buf, screenX, screenY, normDistSq, dx, dy)
		}
	}
}

// buildColorLUT populates the 1D color/power map array (invoked on heat change)
func (p *EmberPainter) buildColorLUT() {
	params := &p.params
	colors := &p.colors

	for i := 0; i < 256; i++ {
		normDist := (int64(i) * vmath.Scale) / 255

		coreT := vmath.Scale - vmath.Mul(normDist, params.CoreFalloff)
		if coreT < 0 {
			coreT = 0
		}
		coreInt := p.powFixed(coreT, params.CorePower)

		midT := vmath.Scale - vmath.Mul(normDist, params.MidFalloff)
		if midT < 0 {
			midT = 0
		}
		midInt := vmath.Mul(p.powFixed(midT, params.MidPower), params.MidIntensity)

		edgeT := vmath.Scale - normDist
		if edgeT < 0 {
			edgeT = 0
		}
		coronaInt := vmath.Mul(p.powFixed(edgeT, params.EdgePower), params.EdgeIntensity)

		p.colorLUT[i] = emberLayerColors{
			Core: scaleRGB(colors.Core, coreInt),
			Mid:  scaleRGB(colors.Mid, midInt),
			Edge: scaleRGB(colors.Edge, coronaInt),
		}
	}
}

// computeJaggedDisplacement returns radius displacement for given angle
func (p *EmberPainter) computeJaggedDisplacement(theta int64) int64 {
	if p.params.JaggedAmp == 0 {
		return 0
	}

	timePhase := (p.gameTime * p.params.JaggedSpeed) / time.Second.Nanoseconds()

	// Multi-octave sine noise
	angle1 := vmath.Mul(theta, p.params.JaggedFreq) + timePhase
	noise := vmath.Sin(angle1) / 2

	angle2 := vmath.Mul(theta, vmath.Mul(p.params.JaggedFreq, 2*vmath.Scale+vmath.Scale/10)) +
		vmath.Mul(timePhase, vmath.Scale+3*vmath.Scale/10)
	noise += vmath.Mul(vmath.Sin(angle2), p.params.Octave2)

	angle3 := vmath.Mul(theta, p.params.JaggedFreq/2) +
		vmath.Mul(timePhase, 7*vmath.Scale/10)
	noise += vmath.Mul(vmath.Sin(angle3), p.params.Octave3)

	// Eruption spikes
	eruptAngle := vmath.Mul(theta, 3*vmath.Scale) + vmath.Mul(timePhase, 3*vmath.Scale/2)
	eruptBase := vmath.Sin(eruptAngle)
	if eruptBase < 0 {
		eruptBase = 0
	}
	eruption := p.powFixed(eruptBase, p.params.EruptionPower)
	eruption = vmath.Mul(eruption, 6*vmath.Scale/5)

	return vmath.Mul(noise+eruption, p.params.JaggedAmp)
}

// emberCellTrueColor renders with layered gradients and rings
func emberCellTrueColor(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, dx, dy int64) {
	normDist := vmath.Sqrt(normDistSq)
	if normDist > vmath.Scale {
		normDist = vmath.Scale
	}

	// Query 1D color mapping cache
	lutIdx := (normDist * 255) >> vmath.Shift
	if lutIdx > 255 {
		lutIdx = 255
	}
	layerColors := &p.colorLUT[lutIdx]

	// Apply corona (additive)
	if layerColors.Edge.R > 0 || layerColors.Edge.G > 0 || layerColors.Edge.B > 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Edge, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Apply mid layer (screen blend)
	if layerColors.Mid.R > 0 || layerColors.Mid.G > 0 || layerColors.Mid.B > 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Mid, render.BlendScreen, 1.0, terminal.AttrNone)
	}

	// Apply core (additive)
	if layerColors.Core.R > 0 || layerColors.Core.G > 0 || layerColors.Core.B > 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Core, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Render rings (only in visible region)
	if p.params.RingAlpha > 0 && normDist < p.params.RingVisible {
		ringVis := p.computeRingVisibility(normDist, dx, dy)
		if ringVis > 0 {
			ringColor := scaleRGB(p.colors.Ring, vmath.FromFloat(ringVis))
			buf.Set(screenX, screenY, 0, visual.RgbBlack, ringColor, render.BlendOverlay, ringVis*0.7, terminal.AttrNone)
		}
	}
}

// computeRingVisibility calculates combined ring visibility algebraically leveraging float math
func (p *EmberPainter) computeRingVisibility(normDist, dx, dy int64) float64 {
	if dx == 0 && dy == 0 {
		return 0
	}

	nx, ny := vmath.Normalize2D(dx, dy)
	nxF := vmath.ToFloat(nx)
	nyF := vmath.ToFloat(ny)

	normDistF := vmath.ToFloat(normDist)
	ringVisF := vmath.ToFloat(p.params.RingVisible)

	edgeFade := 1.0 - (normDistF/ringVisF)*(normDistF/ringVisF)
	if edgeFade < 0 {
		edgeFade = 0
	}

	var maxVis float64
	alphaF := vmath.ToFloat(p.params.RingAlpha)
	dzF := math.Sqrt(math.Max(0, 1.0-normDistF*normDistF))

	for i := 0; i < visual.EmberRingCount; i++ {
		normal := visual.EmberRingNormals[i]
		normX := vmath.ToFloat(normal[0]) // Properly extracts the X component
		normY := vmath.ToFloat(normal[1]) // Properly extracts the Y component
		normZ := vmath.ToFloat(normal[2]) // Properly extracts the Z component

		angle := p.ringAngles[i]
		cosA := vmath.ToFloat(vmath.Cos(angle))
		sinA := vmath.ToFloat(vmath.Sin(angle))

		rz := nxF*sinA*normX + nyF*sinA*normY + dzF*cosA*normZ
		ringDist := math.Abs(rz)

		// Float math avoids two Q32 division operations per cell
		vis := 1.0 / (1.0 + ringDist*ringDist*p.ringInvWidthSq)
		vis *= edgeFade * alphaF

		if rz < -0.1 {
			vis /= 4.0
		}

		if vis > maxVis {
			maxVis = vis
		}
	}

	return maxVis
}

// emberCell256 renders solid ellipse with heat-mapped color
func emberCell256(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, _, _ int64) {
	if normDistSq > vmath.Scale {
		return
	}

	// Derive heat from RingAlpha (0.5 at heat=0, 0 at heat=100)
	heat := 100 - int(vmath.ToFloat(p.params.RingAlpha)*200)
	if heat < 0 {
		heat = 0
	}
	if heat > 100 {
		heat = 100
	}

	buf.SetBg256(screenX, screenY, visual.Ember256PaletteIndex(heat))
}

// powFixed approximates x^n for x in [0, Scale], n in Q32.32
func (p *EmberPainter) powFixed(x, n int64) int64 {
	if x <= 0 {
		return 0
	}
	if x >= vmath.Scale {
		return vmath.Scale
	}
	if n == vmath.Scale {
		return x
	}

	intN := n >> vmath.Shift
	result := vmath.Scale
	base := x

	for i := int64(0); i < intN; i++ {
		result = vmath.Mul(result, base)
	}

	fracN := n & vmath.Mask
	if fracN > 0 {
		nextPow := vmath.Mul(result, base)
		result = result + vmath.Mul(nextPow-result, fracN)
	}

	return result
}

// scaleRGB multiplies RGB by factor in [0, Scale]
func scaleRGB(c terminal.RGB, factor int64) terminal.RGB {
	return terminal.RGB{
		R: uint8((int64(c.R) * factor) >> vmath.Shift),
		G: uint8((int64(c.G) * factor) >> vmath.Shift),
		B: uint8((int64(c.B) * factor) >> vmath.Shift),
	}
}