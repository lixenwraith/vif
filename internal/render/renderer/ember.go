package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/parameter/visual"
	"github.com/lixenwraith/vi-fighter/internal/render"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// radToJaggedIdx maps radians to the 256-bucket jagged/radii LUT index
const radToJaggedIdx = 256.0 / vmath.TwoPi

// emberLayerColors holds pre-blended intensities for cached 1D mapping
type emberLayerColors struct {
	Core color.RGB
	Mid  color.RGB
	Edge color.RGB
}

// emberRingState holds per-ring precomputed values for current frame
type emberRingState struct {
	cosA, sinA float64
	pulseAlpha float64
}

// emberCellFunc renders a single cell within the ember ellipse
type emberCellFunc func(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, dx, dy float64)

// EmberPainter handles per-cell rendering with color mode dispatch
type EmberPainter struct {
	renderCell emberCellFunc

	// Per-Paint state
	params   visual.EmberParams
	colors   emberColors
	gameTime float64
	radiusX  float64
	radiusY  float64

	// Ring rotation state (computed once per paint)
	ringStates [visual.EmberRingCount]emberRingState

	// Cached params (computed once per paint, used per cell)
	ringAlpha        float64
	ringVisibleInvSq float64 // 1/ringVisible² for edge fade
	ringInvWidthSq   float64

	// Caching and Precalculation States
	lastHeat      int
	colorLUT      [256]emberLayerColors
	invRadiiSqLUT [256]struct{ invRxSq, invRySq float64 }
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
	shields := r.gameCtx.World.Components.Shield
	if shields.CountEntities() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskField)

	var cursorEntity core.Entity
	if r.gameCtx.World.Resources.Player.Valid() {
		cursorEntity = r.gameCtx.World.Resources.Player.Entity
	}

	shields.Each(func(entity core.Entity, _ *component.ShieldComponent) bool {
		heatComp, ok := r.gameCtx.World.Components.Heat.GetPtr(entity)
		if !ok || !heatComp.EmberActive {
			return true
		}

		pos, ok := r.gameCtx.World.Positions.GetPosition(entity)
		if !ok {
			return true
		}

		skipX, skipY := -1, -1
		if entity == cursorEntity {
			skipX = pos.X
			skipY = pos.Y
		}

		r.painter.Paint(buf, ctx, pos.X, pos.Y, heatComp.Current, skipX, skipY)
		return true
	})
}

// emberColors holds interpolated colors for current heat level
type emberColors struct {
	Core color.RGB
	Mid  color.RGB
	Edge color.RGB
	Ring color.RGB
}

// interpolateEmberColors computes colors for a normalized heat factor.
func interpolateEmberColors(t float64) emberColors {
	return emberColors{
		Core: render.LerpRGB(visual.RgbEmberCoreLow, visual.RgbEmberCoreHigh, t),
		Mid:  render.LerpRGB(visual.RgbEmberMidLow, visual.RgbEmberMidHigh, t),
		Edge: render.LerpRGB(visual.RgbEmberEdgeLow, visual.RgbEmberEdgeHigh, t),
		Ring: render.LerpRGB(visual.RgbEmberRingLow, visual.RgbEmberRingHigh, t),
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
	p.gameTime = float64(ctx.GameTime.UnixNano()) / 1e9

	// 1D Cache Rebuild: Only on heat change
	if heat != p.lastHeat {
		p.lastHeat = heat
		p.params = visual.InterpolateEmberParams(heat)
		p.colors = interpolateEmberColors(p.params.HeatFactor)
		p.buildColorLUT()
	}

	// Cache geometric reciprocals once per frame.
	if p.params.RingWidth > 0 {
		p.ringInvWidthSq = 1.0 / (p.params.RingWidth * p.params.RingWidth)
	} else {
		p.ringInvWidthSq = 0
	}

	p.ringAlpha = p.params.RingAlpha
	if p.params.RingVisible > 0 {
		p.ringVisibleInvSq = 1.0 / (p.params.RingVisible * p.params.RingVisible)
	} else {
		p.ringVisibleInvSq = 0
	}

	// Compute ring rotation and pulse state once per frame.
	for i := range visual.EmberRingCount {
		effectiveSpeed := p.params.RingSpeed * visual.EmberRingVelocities[i]
		angle := vmath.NormalizeAngleF(p.gameTime*effectiveSpeed + visual.EmberRingPhaseOffsets[i])

		p.ringStates[i].cosA = vmath.CosF(angle)
		p.ringStates[i].sinA = vmath.SinF(angle)
		p.ringStates[i].pulseAlpha = p.ringAlpha + visual.PulseAmplitude*vmath.SinF(p.gameTime*visual.PulseFrequency+visual.EmberRingPulsePhases[i])
	}

	// Precalculate jagged radii and ellipse reciprocals for the frame.
	timePhase := p.gameTime * p.params.JaggedSpeed
	for i := range 256 {
		theta := float64(i) * vmath.TwoPi / 256.0
		disp := p.computeJaggedDisplacement(theta, timePhase)
		adjRx := p.radiusX + disp
		adjRy := p.radiusY + disp/2.0
		invRxSq, invRySq := vmath.EllipseInvRadiiSqF(adjRx, adjRy)
		p.invRadiiSqLUT[i].invRxSq = invRxSq
		p.invRadiiSqLUT[i].invRySq = invRySq
	}

	// Bounding box in map coords with margin for jagged edges
	margin := int(math.Floor(p.params.JaggedAmp)) + 2
	radiusXInt := int(math.Floor(p.radiusX))
	radiusYInt := int(math.Floor(p.radiusY))

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

			cellDX := mapX - centerX
			cellDY := mapY - centerY

			// Atan2F depends only on the ratio, so raw cell deltas are sufficient.
			theta := vmath.Atan2F(float64(cellDY), float64(cellDX))
			lutIdx := int(theta*radToJaggedIdx) & 255
			invRxSq := p.invRadiiSqLUT[lutIdx].invRxSq
			invRySq := p.invRadiiSqLUT[lutIdx].invRySq

			dx := float64(cellDX)
			dy := float64(cellDY)
			normDistSq := vmath.EllipseDistSqF(dx, dy, invRxSq, invRySq)

			if normDistSq > 1.25 {
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

	for i := range 256 {
		normDist := float64(i) / 255.0

		coreT := max(1.0-normDist*params.CoreFalloff, 0.0)
		coreInt := p.powApprox(coreT, params.CorePower)

		midT := max(1.0-normDist*params.MidFalloff, 0.0)
		midInt := p.powApprox(midT, params.MidPower) * params.MidIntensity

		edgeT := max(1.0-normDist, 0.0)
		coronaInt := p.powApprox(edgeT, params.EdgePower) * params.EdgeIntensity

		p.colorLUT[i] = emberLayerColors{
			Core: scaleRGB(colors.Core, coreInt),
			Mid:  scaleRGB(colors.Mid, midInt),
			Edge: scaleRGB(colors.Edge, coronaInt),
		}
	}
}

// computeRingVisibility calculates combined ring visibility
func (p *EmberPainter) computeRingVisibility(normDistF, dxF, dyF float64) float64 {
	// Quadratic edge fade: 1 - (normDist/ringVisible)²
	edgeFade := 1.0 - normDistF*normDistF*p.ringVisibleInvSq
	if edgeFade <= 0 {
		return 0
	}

	dzF := math.Sqrt(math.Max(0, 1.0-normDistF*normDistF))

	var maxVis float64
	for i := range visual.EmberRingCount {
		rs := &p.ringStates[i]
		norms := &visual.EmberRingNormals[i]

		// Ring distance using raw dx, dy
		rz := dxF*rs.sinA*norms[0] + dyF*rs.sinA*norms[1] + dzF*rs.cosA*norms[2]
		ringDistSq := rz * rz

		// Gaussian visibility via ExpDecay LUT
		lutInput := int(ringDistSq * p.ringInvWidthSq * visual.ExpLUTDecayK)
		vis := vmath.ExpDecayF(lutInput) * edgeFade * rs.pulseAlpha

		// Back-face dimming
		if rz < visual.BackFaceThreshold {
			vis *= visual.BackFaceDimming
		}

		if vis > maxVis {
			maxVis = vis
		}
	}

	return maxVis
}

// emberCellTrueColor renders with layered gradients and rings
func emberCellTrueColor(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, dx, dy float64) {
	normDist := min(math.Sqrt(normDistSq), 1.0)

	// Query 1D color mapping cache
	lutIdx := min(int(normDist*255.0), 255)
	layerColors := &p.colorLUT[lutIdx]

	// Apply corona (additive)
	if layerColors.Edge.R|layerColors.Edge.G|layerColors.Edge.B != 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Edge, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Apply mid layer (screen blend)
	if layerColors.Mid.R|layerColors.Mid.G|layerColors.Mid.B != 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Mid, render.BlendScreen, 1.0, terminal.AttrNone)
	}

	// Apply core (additive)
	if layerColors.Core.R|layerColors.Core.G|layerColors.Core.B != 0 {
		buf.Set(screenX, screenY, 0, visual.RgbBlack, layerColors.Core, render.BlendAdd, 1.0, terminal.AttrNone)
	}

	// Render rings
	if p.ringAlpha > 0 {
		ringVis := p.computeRingVisibility(normDist, dx, dy)
		if ringVis > 0.001 {
			ringColor := scaleRGB(p.colors.Ring, ringVis)
			buf.Set(screenX, screenY, 0, visual.RgbBlack, ringColor, render.BlendOverlay, ringVis*0.7, terminal.AttrNone)
		}
	}
}

// computeJaggedDisplacement returns radius displacement for an angle and phase.
func (p *EmberPainter) computeJaggedDisplacement(theta, timePhase float64) float64 {
	if p.params.JaggedAmp == 0 {
		return 0
	}

	// Multi-octave sine noise
	angle1 := theta*p.params.JaggedFreq + timePhase
	noise := vmath.SinF(angle1) / 2.0

	angle2 := theta*(p.params.JaggedFreq*2.1) + timePhase*1.3
	noise += vmath.SinF(angle2) * p.params.Octave2

	angle3 := theta*(p.params.JaggedFreq/2.0) + timePhase*0.7
	noise += vmath.SinF(angle3) * p.params.Octave3

	// Eruption spikes
	eruptAngle := theta*3.0 + timePhase*1.5
	eruptBase := min(vmath.SinF(eruptAngle), 0.0)
	eruption := p.powApprox(eruptBase, p.params.EruptionPower) * 1.2

	return (noise + eruption) * p.params.JaggedAmp
}

// emberCell256 renders solid ellipse with heat-mapped color
func emberCell256(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, _, _ float64) {
	if normDistSq > 1.0 {
		return
	}

	// Derive palette heat from the inverse ring-alpha ramp.
	heat := min(max(100-int(p.params.RingAlpha*200.0), 0), 100)

	buf.SetBg256(screenX, screenY, visual.Ember256PaletteIndex(heat))
}

// powApprox approximates x^n without a per-cell transcendental call.
func (p *EmberPainter) powApprox(x, n float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1.0 {
		return 1.0
	}
	if n == 1.0 {
		return x
	}

	intN := int(math.Floor(n))
	result := 1.0
	base := x

	for range intN {
		result *= base
	}

	fracN := n - float64(intN)
	if fracN > 0 {
		nextPow := result * base
		result += (nextPow - result) * fracN
	}

	return result
}

// scaleRGB multiplies RGB by a normalized intensity factor.
func scaleRGB(c color.RGB, factor float64) color.RGB {
	return color.RGB{
		R: uint8(int(float64(c.R) * factor)),
		G: uint8(int(float64(c.G) * factor)),
		B: uint8(int(float64(c.B) * factor)),
	}
}
