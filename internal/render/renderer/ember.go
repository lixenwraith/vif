package renderer

import (
	"math"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// radToJaggedIdx maps radians to the 256-bucket jagged/radii LUT index
const radToJaggedIdx = 256.0 / vmath.TwoPi

// emberRingThreshold is the ring visibility under which a cell shows no ring
const emberRingThreshold = 0.001

// emberLayerColors is one radial step of the glow: each layer's colour, and the lit
// layers composed over the theme background, which is what most cells start from
type emberLayerColors struct {
	Core, Mid, Edge color.RGB
	Base            color.RGB
	Active          bool
}

// emberRingState holds per-ring precomputed values for current frame
type emberRingState struct {
	cosA, sinA float64
	pulseAlpha float64
}

// emberCellFunc renders a single cell within the ember ellipse
type emberCellFunc func(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, dx, dy float64)

// emberFrameFunc prepares the colour state one Paint needs, rebuilding its caches when told to
type emberFrameFunc func(p *EmberPainter, ctx render.RenderContext, rebuild bool)

// EmberPainter handles per-cell rendering with color mode dispatch
type EmberPainter struct {
	renderCell   emberCellFunc
	prepareFrame emberFrameFunc

	// Per-Paint state
	params     visual.EmberParams
	colors     emberColors
	gameTime   float64
	aspect     vmath.Aspect
	radiusX    float64
	radiusY    float64
	radiusXInt int
	radiusYInt int
	blendScale float64

	// Ring rotation state (computed once per paint)
	ringStates [visual.EmberRingCount]emberRingState

	// Cached params (computed once per paint, used per cell)
	ringAlpha        float64
	ringVisibleInvSq float64 // 1/ringVisible² for edge fade
	ringInvWidthSq   float64
	ringSkipDistSq   float64 // squared plane distance past which a ring cannot show

	// Caching and Precalculation States
	lastHeat      int
	lutBlend      float64 // the blend the colour LUT was composed at
	colorLUT      [256]emberLayerColors
	invRadiiSqLUT [256]struct{ invRxSq, invRySq float64 }
	angleIdx      []uint8 // jagged LUT bucket per cell offset, over the widest box
	angleW        int
	halfW, halfH  int
	palette256    uint8
}

// EmberRenderer renders ember effect for entities with active ember state
type EmberRenderer struct {
	gameCtx   *engine.GameContext
	colorMode terminal.ColorMode
	aspect    vmath.Aspect
	painters  [parameter.MaxPlayers]*EmberPainter
}

// NewEmberRenderer creates the ember system renderer
func NewEmberRenderer(gameCtx *engine.GameContext) *EmberRenderer {
	return &EmberRenderer{
		gameCtx:   gameCtx,
		colorMode: gameCtx.World.Resources.Config.ColorMode,
		aspect:    gameCtx.World.Resources.Config.CellAspect,
	}
}

func (r *EmberRenderer) painter(slot int) *EmberPainter {
	if r.painters[slot] == nil {
		r.painters[slot] = NewEmberPainter(r.colorMode, r.aspect)
	}
	return r.painters[slot]
}

// Render draws all active ember effects
func (r *EmberRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	world := r.gameCtx.World
	shields := world.Components.Shield
	roster := world.Resources.Player
	if shields.CountEntities() == 0 || roster.Count() == 0 {
		return
	}

	buf.SetWriteMask(visual.MaskField)

	localEntity := roster.Entity
	for slot := range parameter.MaxPlayers {
		entity := roster.Slot(uint8(slot))
		if entity == 0 || entity == localEntity {
			continue
		}
		r.renderEmber(ctx, buf, slot, entity, false)
	}
	if localEntity != 0 {
		r.renderEmber(ctx, buf, int(roster.LocalSlot()), localEntity, true)
	}
}

func (r *EmberRenderer) renderEmber(ctx render.RenderContext, buf *render.RenderBuffer, slot int, entity core.Entity, local bool) {
	world := r.gameCtx.World
	if _, ok := world.Components.Shield.GetPtr(entity); !ok {
		return
	}
	heatComp, ok := world.Components.Heat.GetPtr(entity)
	if !ok || !heatComp.EmberActive {
		return
	}

	// D-18 prediction supplies the local cell; every other cursor uses the store.
	pos, ok := world.CursorCell(entity)
	if !ok {
		return
	}

	skipX, skipY := -1, -1
	blendScale := visual.PeerFieldBlend
	if local {
		skipX = pos.X
		skipY = pos.Y
		blendScale = 1
	}
	r.painter(slot).Paint(buf, ctx, pos.X, pos.Y, heatComp.Current, skipX, skipY, blendScale)
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

// NewEmberPainter creates a painter for the specified color mode and cell aspect
func NewEmberPainter(colorMode terminal.ColorMode, aspect vmath.Aspect) *EmberPainter {
	p := &EmberPainter{
		aspect:   aspect,
		radiusX:  visual.EmberRadiusX,
		radiusY:  aspect.Y(visual.EmberRadiusY),
		lastHeat: -1, // Force cache rebuild on first frame
	}
	p.radiusXInt = int(math.Floor(p.radiusX))
	p.radiusYInt = int(math.Floor(p.radiusY))
	p.buildAngleTable()
	if colorMode == terminal.ColorMode256 {
		p.renderCell, p.prepareFrame = emberCell256, emberFrame256
	} else {
		p.renderCell, p.prepareFrame = emberCellTrueColor, emberFrameTrueColor
	}
	return p
}

// emberMargin is the cell margin a jagged amplitude adds around the ellipse
func emberMargin(amp float64) int { return int(math.Floor(amp)) + 2 }

// buildAngleTable maps every cell offset a Paint can visit to its jagged LUT bucket,
// so the per-cell atan2 becomes a load. The box is the ellipse at its widest margin;
// the amplitude is interpolated, so an end of the heat range holds it.
func (p *EmberPainter) buildAngleTable() {
	margin := max(emberMargin(visual.InterpolateEmberParams(0).JaggedAmp), emberMargin(visual.InterpolateEmberParams(100).JaggedAmp))
	p.halfW, p.halfH = p.radiusXInt+margin, p.radiusYInt+margin
	p.angleW = 2*p.halfW + 1
	p.angleIdx = make([]uint8, p.angleW*(2*p.halfH+1))
	for dy := -p.halfH; dy <= p.halfH; dy++ {
		for dx := -p.halfW; dx <= p.halfW; dx++ {
			// Atan2 depends only on the ratio, so raw cell deltas are sufficient
			theta := vmath.Atan2(float64(dy), float64(dx))
			p.angleIdx[(dy+p.halfH)*p.angleW+dx+p.halfW] = uint8(int(theta*radToJaggedIdx) & 255)
		}
	}
}

// Paint renders the ember effect centered at (centerX, centerY) in map coordinates
func (p *EmberPainter) Paint(buf *render.RenderBuffer, ctx render.RenderContext, centerX, centerY int, heat int, skipX, skipY int, blendScale float64) {
	if blendScale <= 0 {
		return
	}
	heatChanged := heat != p.lastHeat
	if heatChanged {
		p.lastHeat = heat
		p.params = visual.InterpolateEmberParams(heat)
	}
	p.blendScale = blendScale
	p.gameTime = float64(ctx.GameTime.UnixNano()) / 1e9
	p.prepareFrame(p, ctx, heatChanged || blendScale != p.lutBlend)

	// Bounding box in map coords with margin for jagged edges, within the visible map
	margin := emberMargin(p.params.JaggedAmp)
	visMinX, visMinY, visMaxX, visMaxY := ctx.VisibleMapBounds()
	mapStartX := max(visMinX, centerX-p.radiusXInt-margin)
	mapEndX := min(visMaxX, centerX+p.radiusXInt+margin)
	mapStartY := max(visMinY, centerY-p.radiusYInt-margin)
	mapEndY := min(visMaxY, centerY+p.radiusYInt+margin)
	if mapStartX > mapEndX || mapStartY > mapEndY {
		return
	}
	p.buildRadiiLUT()

	for mapY := mapStartY; mapY <= mapEndY; mapY++ {
		cellDY := mapY - centerY
		dy := float64(cellDY)
		angleRow := (cellDY+p.halfH)*p.angleW + p.halfW
		for mapX := mapStartX; mapX <= mapEndX; mapX++ {
			if mapX == skipX && mapY == skipY {
				continue
			}

			screenX, screenY, visible := ctx.MapToScreen(mapX, mapY)
			if !visible {
				continue
			}

			cellDX := mapX - centerX
			radii := &p.invRadiiSqLUT[p.angleIdx[angleRow+cellDX]]
			dx := float64(cellDX)
			normDistSq := vmath.EllipseDistSq(dx, dy, radii.invRxSq, radii.invRySq)

			if normDistSq > 1.25 {
				continue
			}

			p.renderCell(p, buf, screenX, screenY, normDistSq, dx, dy)
		}
	}
}

// buildRadiiLUT sets the frame's jagged ellipse reciprocals per angle bucket
func (p *EmberPainter) buildRadiiLUT() {
	if p.params.JaggedAmp == 0 {
		// A still edge is one ellipse at every angle
		invRxSq, invRySq := vmath.EllipseInvRadiiSq(p.radiusX, p.radiusY)
		for i := range p.invRadiiSqLUT {
			p.invRadiiSqLUT[i].invRxSq, p.invRadiiSqLUT[i].invRySq = invRxSq, invRySq
		}
		return
	}
	timePhase := p.gameTime * p.params.JaggedSpeed
	for i := range 256 {
		theta := float64(i) * vmath.TwoPi / 256.0
		disp := p.computeJaggedDisplacement(theta, timePhase)
		invRxSq, invRySq := vmath.EllipseInvRadiiSq(p.radiusX+disp, p.radiusY+p.aspect.FromCircular(disp))
		p.invRadiiSqLUT[i].invRxSq, p.invRadiiSqLUT[i].invRySq = invRxSq, invRySq
	}
}

// emberFrameTrueColor rebuilds the colour LUT when heat or blend changed and advances the rings
func emberFrameTrueColor(p *EmberPainter, _ render.RenderContext, rebuild bool) {
	if rebuild {
		p.colors = interpolateEmberColors(p.params.HeatFactor)
		p.lutBlend = p.blendScale
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
	if rebuild {
		p.ringSkipDistSq = p.ringSkipDist()
	}

	// Compute ring rotation and pulse state once per frame.
	for i := range visual.EmberRingCount {
		effectiveSpeed := p.params.RingSpeed * visual.EmberRingVelocities[i]
		angle := vmath.NormalizeAngle(p.gameTime*effectiveSpeed + visual.EmberRingPhaseOffsets[i])

		p.ringStates[i].cosA = vmath.Cos(angle)
		p.ringStates[i].sinA = vmath.Sin(angle)
		p.ringStates[i].pulseAlpha = p.ringAlpha + visual.PulseAmplitude*vmath.Sin(p.gameTime*visual.PulseFrequency+visual.EmberRingPulsePhases[i])
	}
}

// ringSkipDist is the squared distance from a ring's plane past which its Gaussian,
// at the pulse's peak, stays under emberRingThreshold, so a cell skips the lookup.
// One step of slack covers the rounding between this quotient and a cell's product.
func (p *EmberPainter) ringSkipDist() float64 {
	k := p.ringInvWidthSq * visual.ExpLUTDecayK
	if k <= 0 {
		return math.Inf(1)
	}
	peak := p.ringAlpha + visual.PulseAmplitude
	li := 0
	for li < vmath.ExpLUTMaxInput && vmath.ExpDecay(li)*peak > emberRingThreshold {
		li++
	}
	return float64(li+1) / k
}

// emberFrame256 takes the heat bar's leading colour at the owner's heat, undimmed for a peer
// like its shield
func emberFrame256(p *EmberPainter, ctx render.RenderContext, _ bool) {
	p.palette256 = heatLead256(p.lastHeat, ctx.ScreenWidth)
}

// buildColorLUT populates the 1D color/power map array (invoked on heat or blend change)
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

		layer := &p.colorLUT[i]
		layer.Core = scaleRGB(colors.Core, coreInt)
		layer.Mid = scaleRGB(colors.Mid, midInt)
		layer.Edge = scaleRGB(colors.Edge, coronaInt)
		layer.Base, layer.Active = p.composeLayers(visual.RgbBackground, layer)
	}
}

// composeLayers lays a step's lit layers over bg at the painter's blend; false when none is lit
func (p *EmberPainter) composeLayers(bg color.RGB, layer *emberLayerColors) (color.RGB, bool) {
	edgeActive := layer.Edge.R|layer.Edge.G|layer.Edge.B != 0
	midActive := layer.Mid.R|layer.Mid.G|layer.Mid.B != 0
	coreActive := layer.Core.R|layer.Core.G|layer.Core.B != 0
	if edgeActive {
		bg = color.Add(bg, layer.Edge, p.blendScale)
	}
	if midActive {
		bg = color.Screen(bg, layer.Mid, p.blendScale)
	}
	if coreActive {
		bg = color.Add(bg, layer.Core, p.blendScale)
	}
	return bg, edgeActive || midActive || coreActive
}

// computeRingVisibility calculates combined ring visibility
func (p *EmberPainter) computeRingVisibility(normDistF, dxF, dyF float64) float64 {
	// Quadratic edge fade: 1 - (normDist/ringVisible)²
	edgeFade := 1.0 - normDistF*normDistF*p.ringVisibleInvSq
	if edgeFade <= 0 {
		return 0
	}

	dzF := math.Sqrt(max(0, 1.0-normDistF*normDistF))

	var maxVis float64
	for i := range visual.EmberRingCount {
		rs := &p.ringStates[i]
		norms := &visual.EmberRingNormals[i]

		// Ring distance using raw dx, dy
		rz := dxF*rs.sinA*norms[0] + dyF*rs.sinA*norms[1] + dzF*rs.cosA*norms[2]
		ringDistSq := rz * rz
		if ringDistSq >= p.ringSkipDistSq {
			continue
		}

		// Gaussian visibility via ExpDecay LUT
		lutInput := int(ringDistSq * p.ringInvWidthSq * visual.ExpLUTDecayK)
		vis := vmath.ExpDecay(lutInput) * edgeFade * rs.pulseAlpha

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
	layer := &p.colorLUT[min(int(normDist*255.0), 255)]

	ringVis := 0.0
	if p.ringAlpha > 0 {
		ringVis = p.computeRingVisibility(normDist, dx, dy)
	}
	ringLit := ringVis > emberRingThreshold
	if !layer.Active && !ringLit {
		return
	}

	// A cell at the theme background takes the step's composite as cached
	bg := buf.BackgroundAt(screenX, screenY, visual.RgbBackground)
	if bg == visual.RgbBackground {
		bg = layer.Base
	} else if layer.Active {
		bg, _ = p.composeLayers(bg, layer)
	}
	if ringLit {
		ringColor := scaleRGB(p.colors.Ring, ringVis)
		bg = color.Overlay(bg, ringColor, ringVis*0.7*p.blendScale)
	}
	buf.SetBgOnly(screenX, screenY, bg)
}

// computeJaggedDisplacement returns radius displacement for an angle and phase.
func (p *EmberPainter) computeJaggedDisplacement(theta, timePhase float64) float64 {
	// Multi-octave sine noise
	angle1 := theta*p.params.JaggedFreq + timePhase
	noise := vmath.Sin(angle1) / 2.0

	angle2 := theta*(p.params.JaggedFreq*2.1) + timePhase*1.3
	noise += vmath.Sin(angle2) * p.params.Octave2

	angle3 := theta*(p.params.JaggedFreq/2.0) + timePhase*0.7
	noise += vmath.Sin(angle3) * p.params.Octave3

	return noise * p.params.JaggedAmp
}

// emberCell256 renders solid ellipse with heat-mapped color
func emberCell256(p *EmberPainter, buf *render.RenderBuffer, screenX, screenY int, normDistSq, _, _ float64) {
	if normDistSq > 1.0 {
		return
	}

	buf.SetBg256(screenX, screenY, p.palette256)
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
