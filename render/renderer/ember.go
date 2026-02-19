package renderer

import (
	"time"

	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/parameter/visual"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/terminal"
	"github.com/lixenwraith/vi-fighter/vmath"
)

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

// emberCellFunc renders a single cell within the ember ellipse
type emberCellFunc func(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, theta int64)

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
}

// NewEmberPainter creates a painter for the specified color mode
func NewEmberPainter(colorMode terminal.ColorMode) *EmberPainter {
	p := &EmberPainter{
		radiusX: visual.EmberRadiusX,
		radiusY: visual.EmberRadiusY,
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
	p.params = visual.InterpolateEmberParams(heat)
	p.colors = interpolateEmberColors(p.params.HeatFactor)
	p.gameTime = ctx.GameTime.UnixNano()

	// Compute ring rotation angles based on game time
	for i := 0; i < visual.EmberRingCount; i++ {
		period := time.Second.Nanoseconds()
		phase := (p.gameTime * vmath.Mul(p.params.RingSpeed, vmath.Scale)) / period
		p.ringAngles[i] = vmath.NormalizeAngle(phase + visual.EmberRingPhaseOffsets[i])
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

			theta := p.atan2Fixed(dy, dx)
			jaggedDisp := p.computeJaggedDisplacement(theta)

			adjRx := p.radiusX + jaggedDisp
			adjRy := p.radiusY + vmath.Div(jaggedDisp, 2*vmath.Scale)

			invRxSq, invRySq := vmath.EllipseInvRadiiSq(adjRx, adjRy)
			normDistSq := vmath.EllipseDistSq(dx, dy, invRxSq, invRySq)

			if normDistSq > vmath.Scale+vmath.Scale/4 {
				continue
			}

			p.renderCell(p, buf, screenX, screenY, normDistSq, theta)
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
func emberCellTrueColor(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, theta int64) {
	params := &p.params
	colors := &p.colors

	normDist := vmath.Sqrt(normDistSq)
	if normDist > vmath.Scale {
		normDist = vmath.Scale
	}

	// Core intensity: sharp bright center
	coreT := vmath.Scale - vmath.Mul(normDist, params.CoreFalloff)
	if coreT < 0 {
		coreT = 0
	}
	coreInt := p.powFixed(coreT, params.CorePower)

	// Mid intensity: softer glow
	midT := vmath.Scale - vmath.Mul(normDist, params.MidFalloff)
	if midT < 0 {
		midT = 0
	}
	midInt := vmath.Mul(p.powFixed(midT, params.MidPower), params.MidIntensity)

	// Edge/corona intensity
	edgeT := vmath.Scale - normDist
	coronaInt := vmath.Mul(p.powFixed(edgeT, params.EdgePower), params.EdgeIntensity)

	minThreshold := vmath.Scale / 100
	if coreInt < minThreshold && midInt < minThreshold && coronaInt < minThreshold {
		return
	}

	// Apply corona (additive)
	if coronaInt > minThreshold {
		coronaColor := scaleRGB(colors.Edge, coronaInt)
		buf.Set(screenX, screenY, 0, visual.RgbBlack, coronaColor, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Apply mid layer (screen blend)
	if midInt > minThreshold {
		midColor := scaleRGB(colors.Mid, midInt)
		buf.Set(screenX, screenY, 0, visual.RgbBlack, midColor, render.BlendScreen, 1.0, terminal.AttrNone)
	}

	// Apply core (additive)
	if coreInt > minThreshold {
		coreColor := scaleRGB(colors.Core, coreInt)
		buf.Set(screenX, screenY, 0, visual.RgbBlack, coreColor, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Render rings (only in visible region)
	if params.RingAlpha > minThreshold && normDist < params.RingVisible {
		ringVis := p.computeRingVisibility(normDist, theta)
		if ringVis > minThreshold {
			ringColor := scaleRGB(colors.Ring, ringVis)
			buf.Set(screenX, screenY, 0, visual.RgbBlack, ringColor, render.BlendOverlay, vmath.ToFloat(vmath.Mul(ringVis, 7*vmath.Scale/10)), terminal.AttrNone)
		}
	}
}

// computeRingVisibility calculates combined ring visibility at a point
func (p *EmberPainter) computeRingVisibility(normDist, theta int64) int64 {
	params := &p.params

	edgeFade := vmath.Scale - vmath.Mul(vmath.Div(normDist, params.RingVisible), vmath.Div(normDist, params.RingVisible))
	if edgeFade < 0 {
		edgeFade = 0
	}

	var maxVis int64

	for i := 0; i < visual.EmberRingCount; i++ {
		normal := visual.EmberRingNormals[i]
		angle := p.ringAngles[i]

		cosA := vmath.Cos(angle)
		sinA := vmath.Sin(angle)

		dz := vmath.Sqrt(vmath.Scale - vmath.Mul(normDist, normDist))
		if dz < 0 {
			dz = 0
		}

		dx := vmath.Cos(theta)
		dy := vmath.Sin(theta)

		rz := vmath.Mul(vmath.Mul(dx, sinA), normal[0]) +
			vmath.Mul(vmath.Mul(dy, sinA), normal[1]) +
			vmath.Mul(vmath.Mul(dz, cosA), normal[2])

		ringDist := vmath.Abs(rz)
		widthSq := vmath.Mul(params.RingWidth, params.RingWidth)
		vis := vmath.Div(vmath.Scale, vmath.Scale+vmath.Div(vmath.Mul(ringDist, ringDist), widthSq))
		vis = vmath.Mul(vis, edgeFade)
		vis = vmath.Mul(vis, params.RingAlpha)

		if rz < -vmath.Scale/10 {
			vis = vis / 4
		}

		if vis > maxVis {
			maxVis = vis
		}
	}

	return maxVis
}

// emberCell256 renders solid ellipse with heat-mapped color
func emberCell256(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, _ int64) {
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

// TODO: calculate LUT in vmath

// atan2Fixed returns angle in [0, Scale) for dy/dx using LUT
func (p *EmberPainter) atan2Fixed(dy, dx int64) int64 {
	nx, ny := vmath.Normalize2D(dx, dy)

	bestIdx := 0
	bestDot := -vmath.Scale * 2
	for i := 0; i < 256; i++ {
		lutAngle := int64(i) * vmath.Scale / 256
		cosA := vmath.Cos(lutAngle)
		sinA := vmath.Sin(lutAngle)
		dot := vmath.Mul(nx, cosA) + vmath.Mul(ny, sinA)
		if dot > bestDot {
			bestDot = dot
			bestIdx = i
		}
	}

	return int64(bestIdx) * vmath.Scale / 256
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